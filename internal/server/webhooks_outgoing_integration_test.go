//go:build integration

package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/ssrf"
	"github.com/msch128/mnema-talk/internal/webhook"
)

type received struct {
	header http.Header
	body   []byte
}

// receiver is a webhook target on the loopback interface.
type receiver struct {
	srv    *httptest.Server
	mu     sync.Mutex
	status func(n int, r *http.Request) int
	count  int
	got    chan received
}

func newReceiver(t *testing.T, status func(n int, r *http.Request) int) *receiver {
	t.Helper()
	rc := &receiver{status: status, got: make(chan received, 100)}
	rc.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		rc.mu.Lock()
		rc.count++
		n := rc.count
		rc.mu.Unlock()
		rc.got <- received{header: r.Header.Clone(), body: b}
		code := http.StatusNoContent
		if rc.status != nil {
			code = rc.status(n, r)
		}
		if code == 302 {
			w.Header().Set("Location", "http://169.254.169.254/")
		}
		w.WriteHeader(code)
		_, _ = io.WriteString(w, "SECRET INTERNAL RESPONSE")
	}))
	t.Cleanup(rc.srv.Close)
	return rc
}

func (rc *receiver) next(t *testing.T) received {
	t.Helper()
	select {
	case r := <-rc.got:
		return r
	case <-time.After(5 * time.Second):
		t.Fatal("no delivery arrived")
		return received{}
	}
}

func (rc *receiver) none(t *testing.T) {
	t.Helper()
	select {
	case r := <-rc.got:
		t.Fatalf("unexpected delivery: %s", r.body)
	case <-time.After(300 * time.Millisecond):
	}
}

func allowLoopback(cfg *config.Config) {
	allow, _ := ssrf.ParseAllow("127.0.0.0/8")
	cfg.WebhookAllowPrivateTargets = allow
}

// outgoingApp boots the router with the loopback allowed and fast retries.
func outgoingApp(t *testing.T) *app {
	t.Helper()
	a := newAppWith(t, true, allowLoopback)
	a.router.Webhooks.Outgoing.SetBackoff([]time.Duration{5 * time.Millisecond, 5 * time.Millisecond})
	a.router.Webhooks.SetLimits(1000, 1000)
	return a
}

func makeOutgoing(t *testing.T, c *client, channel uuid.UUID, url string, events ...string) webhook.CreatedOutgoing {
	t.Helper()
	res := c.post("/api/admin/channels/"+channel.String()+"/outgoing-webhooks", map[string]any{"name": "ci", "url": url, "events": events})
	if res.status != http.StatusCreated {
		t.Fatalf("create outgoing: %d %s", res.status, res.body)
	}
	var out webhook.CreatedOutgoing
	res.decode(t, &out)
	return out
}

func getOutgoing(t *testing.T, c *client, channel uuid.UUID, id uuid.UUID) webhook.Outgoing {
	t.Helper()
	res := c.get("/api/admin/channels/" + channel.String() + "/outgoing-webhooks")
	var all []webhook.Outgoing
	res.decode(t, &all)
	for _, o := range all {
		if o.ID == id {
			return o
		}
	}
	t.Fatalf("outgoing webhook %s not listed: %s", id, res.body)
	return webhook.Outgoing{}
}

func deliveriesOf(t *testing.T, c *client, id uuid.UUID) []webhook.Delivery {
	t.Helper()
	res := c.get("/api/admin/outgoing-webhooks/" + id.String() + "/deliveries")
	var list []webhook.Delivery
	res.decode(t, &list)
	return list
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestOutgoingDeliversSignedEvents(t *testing.T) {
	a := outgoingApp(t)
	admin := a.seedAdmin()
	ch := a.createChannel(admin, "alerts", "text")
	other := a.createChannel(admin, "other", "text")
	rc := newReceiver(t, nil)
	o := makeOutgoing(t, admin, ch, rc.srv.URL+"/hook", "MESSAGE_CREATE", "MESSAGE_DELETE", "MESSAGE_CREATE")
	if !strings.HasPrefix(o.Secret, "whsec_") || len(o.Events) != 2 || o.Status != "active" {
		t.Fatalf("created %+v", o)
	}
	if strings.Contains(string(admin.get("/api/admin/channels/"+ch.String()+"/outgoing-webhooks").body), o.Secret) {
		t.Fatal("list leaks the secret")
	}

	id := a.send(admin, ch, "hello **world**")
	got := rc.next(t)
	if got.header.Get("X-Mnema-Event") != "MESSAGE_CREATE" || got.header.Get("Content-Type") != "application/json" ||
		!strings.HasPrefix(got.header.Get("User-Agent"), "MnemaTalk-Webhook/") || got.header.Get("X-Mnema-Delivery") == "" {
		t.Fatalf("headers %v", got.header)
	}
	sig := got.header.Get("X-Mnema-Signature")
	parts := strings.Split(sig, ",")
	if len(parts) != 2 || !strings.HasPrefix(parts[0], "t=") || !strings.HasPrefix(parts[1], "v1=") {
		t.Fatalf("signature %q", sig)
	}
	var ts int64
	if _, err := fmtSscan(strings.TrimPrefix(parts[0], "t="), &ts); err != nil {
		t.Fatal(err)
	}
	if want := webhook.Sign(o.Secret, time.Unix(ts, 0), got.body); want != sig {
		t.Fatalf("signature does not verify: %q vs %q", sig, want)
	}
	if webhook.Sign("whsec_other", time.Unix(ts, 0), got.body) == sig {
		t.Fatal("a different secret verifies")
	}
	var env webhook.Envelope
	if err := json.Unmarshal(got.body, &env); err != nil {
		t.Fatal(err)
	}
	var d struct {
		ID      uuid.UUID `json:"id"`
		Content string    `json:"content"`
	}
	_ = json.Unmarshal(env.D, &d)
	if env.T != "MESSAGE_CREATE" || env.WebhookID != o.ID.String() || env.ChannelID == nil || *env.ChannelID != ch.String() ||
		env.ID != got.header.Get("X-Mnema-Delivery") || d.ID != id || d.Content != "hello **world**" {
		t.Fatalf("envelope %s", got.body)
	}

	// Other channels, other events and webhook-made messages stay out.
	a.send(admin, other, "elsewhere")
	if res := admin.put("/api/channels/"+ch.String()+"/messages/"+id.String(), map[string]any{"content": "edited"}); res.status != 200 {
		t.Fatalf("edit: %d %s", res.status, res.body)
	}
	in := makeHook(t, admin, ch, "in")
	a.exec(in.path, map[string]any{"content": "from a webhook"}, nil)
	rc.none(t)
	if res := admin.delete("/api/channels/" + ch.String() + "/messages/" + id.String()); res.status != 204 {
		t.Fatalf("delete: %d %s", res.status, res.body)
	}
	if got := rc.next(t); got.header.Get("X-Mnema-Event") != "MESSAGE_DELETE" {
		t.Fatalf("event %s", got.header.Get("X-Mnema-Event"))
	}

	waitFor(t, "the log", func() bool { return len(deliveriesOf(t, admin, o.ID)) == 2 })
	log := deliveriesOf(t, admin, o.ID)
	if !log[0].Success || log[0].Attempt != 1 || log[0].StatusCode == nil || *log[0].StatusCode != 204 || log[0].Error != nil {
		t.Fatalf("log %+v", log)
	}
	if strings.Contains(string(admin.get("/api/admin/outgoing-webhooks/"+o.ID.String()+"/deliveries").body), "SECRET INTERNAL") {
		t.Fatal("the receiver's response was stored")
	}
	cur := getOutgoing(t, admin, ch, o.ID)
	if cur.LastSuccessAt == nil || cur.ConsecutiveFailures != 0 || cur.LastError != nil {
		t.Fatalf("state %+v", cur)
	}
}

func TestOutgoingRetriesAndSwitchesOffAfterRepeatedFailures(t *testing.T) {
	a := outgoingApp(t)
	admin := a.seedAdmin()
	ch := a.createChannel(admin, "alerts", "text")

	// A transient failure: the retry arrives and succeeds, nothing is counted.
	flaky := newReceiver(t, func(n int, _ *http.Request) int {
		if n == 1 {
			return 503
		}
		return 200
	})
	f := makeOutgoing(t, admin, ch, flaky.srv.URL, "MESSAGE_CREATE")
	a.send(admin, ch, "one")
	first := flaky.next(t)
	second := flaky.next(t)
	if first.header.Get("X-Mnema-Delivery") != second.header.Get("X-Mnema-Delivery") {
		t.Fatal("a retry is a different delivery")
	}
	waitFor(t, "two attempts", func() bool { return len(deliveriesOf(t, admin, f.ID)) == 2 })
	log := deliveriesOf(t, admin, f.ID)
	if log[0].Attempt != 2 || !log[0].Success || log[1].Attempt != 1 || log[1].Success || *log[1].Error != "http_503" {
		t.Fatalf("log %+v", log)
	}
	if cur := getOutgoing(t, admin, ch, f.ID); cur.ConsecutiveFailures != 0 || cur.Status != "active" {
		t.Fatalf("state %+v", cur)
	}
	admin.delete("/api/admin/outgoing-webhooks/" + f.ID.String())

	// A dead receiver: each event is tried three times (the backoff list has
	// two entries), and five lost events in a row switch the webhook off.
	dead := newReceiver(t, func(int, *http.Request) int { return 500 })
	o := makeOutgoing(t, admin, ch, dead.srv.URL, "MESSAGE_CREATE")
	for i := 0; i < 5; i++ {
		a.send(admin, ch, "event")
		dead.next(t)
		dead.next(t)
		dead.next(t)
		want := i + 1
		waitFor(t, "failure to be counted", func() bool { return getOutgoing(t, admin, ch, o.ID).ConsecutiveFailures >= want })
	}
	cur := getOutgoing(t, admin, ch, o.ID)
	if cur.Status != "auto_disabled" || cur.ConsecutiveFailures != 5 || cur.LastError == nil || *cur.LastError != "http_500" {
		t.Fatalf("state %+v", cur)
	}
	a.send(admin, ch, "after")
	dead.none(t)

	// The log is bounded.
	if n := len(deliveriesOf(t, admin, o.ID)); n != 15 {
		t.Fatalf("log has %d entries", n)
	}
	// Switching it on again clears the state; a test event works.
	res := admin.patch("/api/admin/outgoing-webhooks/"+o.ID.String(), map[string]any{"enabled": true})
	var on webhook.Outgoing
	res.decode(t, &on)
	if res.status != 200 || on.Status != "active" || on.ConsecutiveFailures != 0 {
		t.Fatalf("re-enable: %d %s", res.status, res.body)
	}
	a.send(admin, ch, "again")
	dead.next(t)
}

func TestOutgoingDoesNotRetryClientErrorsOrRedirects(t *testing.T) {
	a := outgoingApp(t)
	admin := a.seedAdmin()
	ch := a.createChannel(admin, "alerts", "text")
	for status, code := range map[int]string{404: "http_404", 302: "redirect_not_followed"} {
		rc := newReceiver(t, func(int, *http.Request) int { return status })
		o := makeOutgoing(t, admin, ch, rc.srv.URL, "MESSAGE_CREATE")
		a.send(admin, ch, "x")
		rc.next(t)
		rc.none(t)
		waitFor(t, "the log", func() bool { return len(deliveriesOf(t, admin, o.ID)) == 1 })
		d := deliveriesOf(t, admin, o.ID)[0]
		if d.Success || d.Error == nil || *d.Error != code || d.Attempt != 1 {
			t.Fatalf("%d: %+v", status, d)
		}
		admin.delete("/api/admin/outgoing-webhooks/" + o.ID.String())
	}
}

// An outgoing webhook belongs to its channel and sends that channel's events:
// it does not depend on who made it, and members who cannot see the channel
// cannot see or manage it.
func TestOutgoingBelongsToItsChannel(t *testing.T) {
	a := outgoingApp(t)
	admin := a.seedAdmin()
	ch := a.createChannel(admin, "alerts", "text")
	other := a.createChannel(admin, "other", "text")
	rc := newReceiver(t, nil)

	role := createRole(t, admin, "hooks", perm.ManageWebhooks)
	mod := a.register(admin, "mod")
	assignRoles(t, admin, mod, role)
	o := makeOutgoing(t, mod, ch, rc.srv.URL, "MESSAGE_CREATE")
	a.send(admin, ch, "one")
	rc.next(t)
	a.send(admin, other, "elsewhere")
	rc.none(t)

	// Whoever made it losing the role, or the account, changes nothing.
	assignRoles(t, admin, mod)
	a.send(admin, ch, "two")
	rc.next(t)
	if res := admin.post("/api/admin/users/"+mod.user.ID.String()+"/disable", nil); res.status >= 300 {
		t.Fatalf("disable: %d", res.status)
	}
	a.send(admin, ch, "three")
	rc.next(t)

	// Another manager of the channel's webhooks may manage it, whatever their
	// rank, including the Owner's.
	peer := a.register(admin, "peer")
	assignRoles(t, admin, peer, role)
	if r := peer.patch("/api/admin/outgoing-webhooks/"+o.ID.String(), map[string]any{"name": "taken over"}); r.status != http.StatusOK {
		t.Fatalf("peer edits: %d %s", r.status, r.body)
	}
	var rot webhook.CreatedOutgoing
	if r := peer.post("/api/admin/outgoing-webhooks/"+o.ID.String()+"/secret", nil); r.status != http.StatusOK {
		t.Fatalf("peer rotates: %d %s", r.status, r.body)
	} else {
		r.decode(t, &rot)
	}
	if rot.CreatedBy == nil || *rot.CreatedBy != mod.user.ID {
		t.Fatalf("rotating the secret changed the record of who made it: %v", rot.CreatedBy)
	}
	owners := makeOutgoing(t, admin, ch, rc.srv.URL, "MESSAGE_DELETE")
	if r := peer.delete("/api/admin/outgoing-webhooks/" + owners.ID.String()); r.status != http.StatusNoContent {
		t.Fatalf("peer deletes the Owner's: %d", r.status)
	}

	// A private channel hides its webhooks from those who cannot view it.
	everyone := everyoneID(t, admin)
	if res := putOverwrites(admin, "channels", ch, overwrite("role", everyone, 0, perm.ViewChannel)); res.status != 204 {
		t.Fatalf("overwrite: %d %s", res.status, res.body)
	}
	bystander := a.register(admin, "bystander")
	assignRoles(t, admin, bystander, role)
	if r := bystander.get("/api/admin/channels/" + ch.String() + "/outgoing-webhooks"); r.status != http.StatusNotFound {
		t.Fatalf("list in a hidden channel: %d", r.status)
	}
	if r := bystander.delete("/api/admin/outgoing-webhooks/" + o.ID.String()); r.status != http.StatusNotFound {
		t.Fatalf("delete in a hidden channel: %d", r.status)
	}
	// There is no server-wide scope.
	if r := admin.post("/api/admin/outgoing-webhooks", map[string]any{"name": "x", "url": rc.srv.URL, "events": []string{"MESSAGE_CREATE"}}); r.status < 400 {
		t.Fatalf("server-wide webhook created: %d", r.status)
	}
	if r := admin.post("/api/admin/channels/"+ch.String()+"/outgoing-webhooks", map[string]any{"name": "x", "url": rc.srv.URL, "events": []string{"MEMBER_ADD"}}); r.status != 400 {
		t.Fatalf("member events are not offered: %d", r.status)
	}
}

func TestOutgoingTargetsAreGuarded(t *testing.T) {
	// No allowlist: only public addresses.
	a := newAppWith(t, true, nil)
	admin := a.seedAdmin()
	ch := a.createChannel(admin, "alerts", "text")
	list := "/api/admin/channels/" + ch.String() + "/outgoing-webhooks"
	for _, u := range []string{
		"http://127.0.0.1:9/x", "http://[::1]/x", "http://10.0.0.1/", "http://192.168.1.1/", "http://169.254.169.254/latest/meta-data/",
		"http://[fd00:ec2::254]/", "http://[::ffff:127.0.0.1]/", "http://0.0.0.0/", "ftp://example.com/", "file:///etc/passwd", "https://user:pw@example.com/", "javascript:alert(1)", "",
	} {
		res := admin.post(list, map[string]any{"name": "x", "url": u, "events": []string{"MESSAGE_CREATE"}})
		if res.status != 400 {
			t.Errorf("%q accepted: %d %s", u, res.status, res.body)
		}
	}
	// A name that resolves to the loopback passes the syntax check and is
	// refused when it is connected to.
	o := makeOutgoing(t, admin, ch, "http://localhost:9/x", "MESSAGE_CREATE")
	res := admin.post("/api/admin/outgoing-webhooks/"+o.ID.String()+"/test", nil)
	var d webhook.Delivery
	res.decode(t, &d)
	if res.status != 200 || d.Success || d.Error == nil || *d.Error != "blocked_address" || d.EventType != "PING" {
		t.Fatalf("test event: %d %s", res.status, res.body)
	}
	// It is not retried and the next event is refused the same way.
	a.send(admin, ch, "x")
	waitFor(t, "the refusal", func() bool { return getOutgoing(t, admin, ch, o.ID).ConsecutiveFailures == 1 })
	if cur := getOutgoing(t, admin, ch, o.ID); cur.LastError == nil || *cur.LastError != "blocked_address" || cur.LastStatus != nil {
		t.Fatalf("state %+v", cur)
	}
	// Updating the URL runs the same check.
	if r := admin.patch("/api/admin/outgoing-webhooks/"+o.ID.String(), map[string]any{"url": "http://10.1.1.1/"}); r.status != 400 {
		t.Fatalf("patch to a private address: %d", r.status)
	}
}

func TestOutgoingManagement(t *testing.T) {
	a := outgoingApp(t)
	admin := a.seedAdmin()
	ch := a.createChannel(admin, "alerts", "text")
	rc := newReceiver(t, nil)
	alice := a.register(admin, "alice")
	list := "/api/admin/channels/" + ch.String() + "/outgoing-webhooks"

	if r := alice.get(list); r.status != 403 {
		t.Fatalf("member lists: %d", r.status)
	}
	if r := alice.post(list, map[string]any{"name": "x", "url": rc.srv.URL, "events": []string{"MESSAGE_CREATE"}}); r.status != 403 {
		t.Fatalf("member creates: %d", r.status)
	}
	bad := []map[string]any{
		{"name": "", "url": rc.srv.URL, "events": []string{"MESSAGE_CREATE"}},
		{"name": "x", "url": rc.srv.URL, "events": []string{}},
		{"name": "x", "url": rc.srv.URL, "events": []string{"PING"}},
		{"name": "x", "url": rc.srv.URL, "events": []string{"NOPE"}},
		{"name": "x", "url": rc.srv.URL},
		{"name": "x", "url": rc.srv.URL, "events": []string{"MESSAGE_CREATE"}, "surprise": 1},
	}
	for _, b := range bad {
		if r := admin.post(list, b); r.status != 400 {
			t.Errorf("%v accepted: %d", b, r.status)
		}
	}
	o := makeOutgoing(t, admin, ch, rc.srv.URL, "MESSAGE_CREATE")

	// Rename, new events, off and on.
	res := admin.patch("/api/admin/outgoing-webhooks/"+o.ID.String(), map[string]any{"name": "renamed", "events": []string{"MESSAGE_UPDATE"}, "url": rc.srv.URL + "/v2", "enabled": false})
	var cur webhook.Outgoing
	res.decode(t, &cur)
	if res.status != 200 || cur.Name != "renamed" || cur.Status != "disabled" || len(cur.Events) != 1 || !strings.HasSuffix(cur.URL, "/v2") {
		t.Fatalf("patch: %d %s", res.status, res.body)
	}
	a.send(admin, ch, "ignored")
	rc.none(t)
	for _, b := range []map[string]any{{"name": ""}, {"url": "ftp://x"}, {"events": []string{"X"}}, {"unknown": 1}} {
		if r := admin.patch("/api/admin/outgoing-webhooks/"+o.ID.String(), b); r.status != 400 {
			t.Errorf("patch %v: %d", b, r.status)
		}
	}
	admin.patch("/api/admin/outgoing-webhooks/"+o.ID.String(), map[string]any{"enabled": true})

	// The test button sends a PING (also with the webhook off) and is limited.
	for i := 0; i < 6; i++ {
		r := admin.post("/api/admin/outgoing-webhooks/"+o.ID.String()+"/test", nil)
		if r.status != 200 {
			t.Fatalf("test %d: %d %s", i, r.status, r.body)
		}
		if got := rc.next(t); got.header.Get("X-Mnema-Event") != "PING" {
			t.Fatalf("event %s", got.header.Get("X-Mnema-Event"))
		}
	}
	if r := admin.post("/api/admin/outgoing-webhooks/"+o.ID.String()+"/test", nil); r.status != 429 {
		t.Fatalf("test button is not limited: %d", r.status)
	}
	if cur := getOutgoing(t, admin, ch, o.ID); cur.ConsecutiveFailures != 0 || cur.LastAttemptAt != nil {
		t.Fatalf("test events must not change the state: %+v", cur)
	}

	// Rotating the secret changes the signature key.
	var rot webhook.CreatedOutgoing
	admin.post("/api/admin/outgoing-webhooks/"+o.ID.String()+"/secret", nil).decode(t, &rot)
	if rot.Secret == "" || rot.Secret == o.Secret {
		t.Fatalf("rotate: %+v", rot)
	}
	admin.patch("/api/admin/outgoing-webhooks/"+o.ID.String(), map[string]any{"events": []string{"MESSAGE_CREATE"}})
	a.send(admin, ch, "signed with the new secret")
	got := rc.next(t)
	var ts int64
	_, _ = fmtSscan(strings.TrimPrefix(strings.Split(got.header.Get("X-Mnema-Signature"), ",")[0], "t="), &ts)
	if webhook.Sign(rot.Secret, time.Unix(ts, 0), got.body) != got.header.Get("X-Mnema-Signature") {
		t.Fatal("not signed with the rotated secret")
	}

	// Unknown ids, bad ids, the cap, deletion.
	for _, p := range []string{"/secret", "/test", "/deliveries", ""} {
		method := http.MethodPost
		if p == "/deliveries" {
			method = http.MethodGet
		}
		if p == "" {
			method = http.MethodDelete
		}
		if r := admin.do(method, "/api/admin/outgoing-webhooks/"+uuid.NewString()+p, nil, nil); r.status != 404 {
			t.Errorf("unknown %s: %d", p, r.status)
		}
		if r := admin.do(method, "/api/admin/outgoing-webhooks/nope"+p, nil, nil); r.status != 400 {
			t.Errorf("bad id %s: %d", p, r.status)
		}
	}
	spare := newReceiver(t, nil)
	hit := false
	for i := 0; i < 12 && !hit; i++ {
		hit = admin.post(list, map[string]any{"name": "n", "url": spare.srv.URL, "events": []string{"MESSAGE_CREATE"}}).status == 409
	}
	if !hit {
		t.Fatal("no cap on outgoing webhooks per channel")
	}
	if r := admin.delete("/api/admin/outgoing-webhooks/" + o.ID.String()); r.status != 204 {
		t.Fatalf("delete: %d", r.status)
	}
	a.send(admin, ch, "gone")
	rc.none(t)
	// Deleting the channel removes its outgoing webhooks.
	if r := admin.delete("/api/admin/channels/" + ch.String()); r.status != 204 {
		t.Fatalf("delete channel: %d", r.status)
	}
	var n int
	if err := a.db.QueryRow(t.Context(), `SELECT COUNT(*) FROM outgoing_webhooks WHERE channel_id = $1`, ch).Scan(&n); err != nil || n != 0 {
		t.Fatalf("webhooks of a deleted channel: %d %v", n, err)
	}
}

func fmtSscan(s string, v *int64) (int, error) {
	n, err := strconv.ParseInt(s, 10, 64)
	*v = n
	return 1, err
}
