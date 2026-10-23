//go:build integration

package server

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/push"
)

// fakePushService stands in for every browser vendor's push service: the
// router's push client sends everything to it, whatever the endpoint says.
type fakePushService struct {
	srv    *httptest.Server
	mu     sync.Mutex
	paths  []string
	status int
}

func newFakePushService(t *testing.T) *fakePushService {
	t.Helper()
	f := &fakePushService{status: http.StatusCreated}
	f.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		f.mu.Lock()
		f.paths = append(f.paths, r.URL.Path)
		status := f.status
		f.mu.Unlock()
		w.WriteHeader(status)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// Do sends the request to the fake instead of the host it names.
func (f *fakePushService) Do(req *http.Request) (*http.Response, error) {
	target, _ := url.Parse(f.srv.URL)
	req.URL.Scheme, req.URL.Host = target.Scheme, target.Host
	return f.srv.Client().Do(req)
}

func (f *fakePushService) received() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.paths...)
}

func (f *fakePushService) waitFor(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, got := range f.received() {
			if got == path {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("nothing reached %s; the push service saw %v", path, f.received())
}

func newPushApp(t *testing.T, withHub bool, adjust func(*config.Config)) (*app, *fakePushService) {
	t.Helper()
	fake := newFakePushService(t)
	a := newAppWithDeps(t, withHub, adjust, func(d *Deps) {
		d.PushClient = fake
		d.PushBurst = 20 * time.Millisecond
	})
	return a, fake
}

// subscription is the body a browser's PushSubscription makes.
func subscription(t *testing.T, path string) map[string]any {
	t.Helper()
	key, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	secret := make([]byte, 16)
	_, _ = rand.Read(secret)
	return map[string]any{
		"endpoint": "https://push.example.com" + path,
		"p256dh":   base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()),
		"auth":     base64.RawURLEncoding.EncodeToString(secret),
		"label":    "Firefox on Linux",
	}
}

type pushConfig struct {
	Enabled        bool   `json:"enabled"`
	PublicKey      string `json:"public_key"`
	PreviewAllowed bool   `json:"preview_allowed"`
	Preview        bool   `json:"preview"`
	MaxDevices     int    `json:"max_devices"`
}

type pushDevices struct {
	Subscriptions []push.Subscription `json:"subscriptions"`
}

func TestPushAPIRequiresASession(t *testing.T) {
	a, _ := newPushApp(t, false, nil)
	anon := a.anon()
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/api/push/config"}, {http.MethodGet, "/api/push/subscriptions"},
		{http.MethodPost, "/api/push/subscriptions"}, {http.MethodPut, "/api/push/preferences"}, {http.MethodPost, "/api/push/test"},
	} {
		if res := anon.do(route.method, route.path, map[string]any{}, nil); res.status != http.StatusUnauthorized {
			t.Errorf("%s %s without a session: %d", route.method, route.path, res.status)
		}
	}
}

func TestAMemberTurnsPushOnAndOffForADevice(t *testing.T) {
	a, _ := newPushApp(t, false, nil)
	admin := a.seedAdmin()
	bob := a.register(admin, "bob")

	var cfg pushConfig
	res := bob.get("/api/push/config")
	wantStatus(t, "config", res, http.StatusOK)
	res.decode(t, &cfg)
	if raw, err := base64.RawURLEncoding.DecodeString(cfg.PublicKey); !cfg.Enabled || err != nil || len(raw) != 65 || !cfg.PreviewAllowed || !cfg.Preview || cfg.MaxDevices != 10 {
		t.Fatalf("config %+v", cfg)
	}

	body := subscription(t, "/wpush/v2/bob-laptop")
	res = bob.post("/api/push/subscriptions", body)
	wantStatus(t, "subscribe", res, http.StatusCreated)
	var sub push.Subscription
	res.decode(t, &sub)
	if sub.Label != "Firefox on Linux" || sub.Fingerprint != push.Fingerprint("https://push.example.com/wpush/v2/bob-laptop") {
		t.Fatalf("%+v", sub)
	}
	for _, secret := range []string{"push.example.com", body["p256dh"].(string), body["auth"].(string)} {
		if strings.Contains(string(res.body), secret) {
			t.Fatalf("the response reveals %q: %s", secret, res.body)
		}
	}

	var list pushDevices
	res = bob.get("/api/push/subscriptions")
	wantStatus(t, "list", res, http.StatusOK)
	res.decode(t, &list)
	if len(list.Subscriptions) != 1 || list.Subscriptions[0].ID != sub.ID {
		t.Fatalf("list %+v", list)
	}
	var others pushDevices
	admin.get("/api/push/subscriptions").decode(t, &others)
	if len(others.Subscriptions) != 0 {
		t.Fatalf("another account sees %+v", others)
	}

	// Someone else cannot remove it; its owner can.
	wantStatus(t, "other account", admin.delete("/api/push/subscriptions/"+sub.ID.String()), http.StatusNotFound)
	wantStatus(t, "own device", bob.delete("/api/push/subscriptions/"+sub.ID.String()), http.StatusNoContent)
	wantStatus(t, "again", bob.delete("/api/push/subscriptions/"+sub.ID.String()), http.StatusNotFound)
	wantStatus(t, "bad id", bob.delete("/api/push/subscriptions/nope"), http.StatusBadRequest)
}

func TestPushRefusesEndpointsThatCouldBeUsedAsARequestProxy(t *testing.T) {
	a, fake := newPushApp(t, false, nil)
	admin := a.seedAdmin()
	bob := a.register(admin, "bob")
	for _, endpoint := range []string{
		"http://push.example.com/x", "https://127.0.0.1/x", "https://10.1.2.3/x", "https://169.254.169.254/latest",
		"https://[::1]/x", "https://push.example.com:6379/x", "https://u:p@push.example.com/x", "ftp://push.example.com/x", "",
	} {
		body := subscription(t, "/x")
		body["endpoint"] = endpoint
		res := bob.post("/api/push/subscriptions", body)
		if res.status != http.StatusBadRequest || res.code(t) != "INVALID_INPUT" {
			t.Errorf("%q: %d %s", endpoint, res.status, res.body)
		}
	}
	bad := subscription(t, "/x")
	bad["p256dh"] = "AAAA"
	wantStatus(t, "bad key", bob.post("/api/push/subscriptions", bad), http.StatusBadRequest)
	extra := subscription(t, "/x")
	extra["unknown"] = true
	wantStatus(t, "unknown field", bob.post("/api/push/subscriptions", extra), http.StatusBadRequest)
	if got := fake.received(); len(got) != 0 {
		t.Fatalf("the server contacted %v while only refusing", got)
	}
}

func TestADeviceLimitAndMovingABrowserBetweenAccounts(t *testing.T) {
	a, _ := newPushApp(t, false, nil)
	admin := a.seedAdmin()
	bob := a.register(admin, "bob")
	for i := range push.MaxDevices {
		wantStatus(t, "device", bob.post("/api/push/subscriptions", subscription(t, "/d"+strconv.Itoa(i))), http.StatusCreated)
	}
	over := bob.post("/api/push/subscriptions", subscription(t, "/one-too-many"))
	if over.status != http.StatusConflict || over.code(t) != "CONFLICT" {
		t.Fatalf("%d %s", over.status, over.body)
	}

	// The same browser, signed in as another account, takes the device over.
	shared := subscription(t, "/d0")
	wantStatus(t, "carol signs in on bob's browser", admin.post("/api/push/subscriptions", shared), http.StatusCreated)
	var list pushDevices
	bob.get("/api/push/subscriptions").decode(t, &list)
	if len(list.Subscriptions) != push.MaxDevices-1 {
		t.Fatalf("bob keeps %d devices after one moved away", len(list.Subscriptions))
	}
}

func TestThePreviewChoiceAndTheOperatorsOverride(t *testing.T) {
	a, _ := newPushApp(t, false, nil)
	bob := a.register(a.seedAdmin(), "bob")
	wantStatus(t, "off", bob.put("/api/push/preferences", map[string]any{"preview": false}), http.StatusNoContent)
	var cfg pushConfig
	bob.get("/api/push/config").decode(t, &cfg)
	if cfg.Preview || !cfg.PreviewAllowed {
		t.Fatalf("%+v", cfg)
	}
	wantStatus(t, "unknown field", bob.put("/api/push/preferences", map[string]any{"preview": true, "x": 1}), http.StatusBadRequest)

	forced, _ := newPushApp(t, false, func(c *config.Config) { c.PushPreviews = "off" })
	member := forced.register(forced.seedAdmin(), "bob")
	var forcedCfg pushConfig
	member.get("/api/push/config").decode(t, &forcedCfg)
	if forcedCfg.PreviewAllowed {
		t.Fatalf("operator override not reported: %+v", forcedCfg)
	}
}

func TestWithPushTurnedOffThereIsNoKeyAndNoDevicesAPI(t *testing.T) {
	a, fake := newPushApp(t, true, func(c *config.Config) { c.PushEnabled = false })
	admin := a.seedAdmin()
	bob := a.register(admin, "bob")
	var cfg pushConfig
	res := bob.get("/api/push/config")
	wantStatus(t, "config", res, http.StatusOK)
	res.decode(t, &cfg)
	if cfg.Enabled || cfg.PublicKey != "" {
		t.Fatalf("%+v", cfg)
	}
	wantStatus(t, "list", bob.get("/api/push/subscriptions"), http.StatusNotFound)
	wantStatus(t, "subscribe", bob.post("/api/push/subscriptions", subscription(t, "/x")), http.StatusNotFound)
	wantStatus(t, "delete", bob.delete("/api/push/subscriptions/00000000-0000-4000-8000-000000000001"), http.StatusNotFound)
	wantStatus(t, "preferences", bob.put("/api/push/preferences", map[string]any{"preview": false}), http.StatusNotFound)
	wantStatus(t, "test", bob.post("/api/push/test", nil), http.StatusNotFound)

	ch := a.createChannel(admin, "general", "text")
	a.send(admin, ch, "hello")
	time.Sleep(100 * time.Millisecond)
	if got := fake.received(); len(got) != 0 {
		t.Fatalf("push is off but %v were sent", got)
	}
}

func TestTheTestButtonReportsWhatReachedThePushService(t *testing.T) {
	a, fake := newPushApp(t, false, nil)
	bob := a.register(a.seedAdmin(), "bob")
	wantStatus(t, "subscribe", bob.post("/api/push/subscriptions", subscription(t, "/phone")), http.StatusCreated)
	var res push.TestResult
	r := bob.post("/api/push/test", nil)
	wantStatus(t, "test", r, http.StatusOK)
	r.decode(t, &res)
	if res.Delivered != 1 || res.Failed != 0 {
		t.Fatalf("%+v", res)
	}
	fake.waitFor(t, "/phone")

	fake.mu.Lock()
	fake.status = http.StatusGone
	fake.mu.Unlock()
	r = bob.post("/api/push/test", nil)
	r.decode(t, &res)
	if res.Delivered != 0 || res.Failed != 1 {
		t.Fatalf("%+v", res)
	}
	var list pushDevices
	bob.get("/api/push/subscriptions").decode(t, &list)
	if len(list.Subscriptions) != 0 {
		t.Fatalf("a device the push service no longer knows is still listed: %+v", list)
	}
}

func TestTheTestButtonIsRateLimited(t *testing.T) {
	a, _ := newPushApp(t, false, nil)
	bob := a.register(a.seedAdmin(), "bob")
	var limited bool
	for range 8 {
		if bob.post("/api/push/test", nil).status == http.StatusTooManyRequests {
			limited = true
		}
	}
	if !limited {
		t.Fatal("eight test pushes in a row were all allowed")
	}
}

func TestANewMessageIsPushedThroughTheRealHubToOtherMembersOnly(t *testing.T) {
	a, fake := newPushApp(t, true, nil)
	admin := a.seedAdmin()
	bob := a.register(admin, "bob")
	wantStatus(t, "bob", bob.post("/api/push/subscriptions", subscription(t, "/bob-phone")), http.StatusCreated)
	wantStatus(t, "admin", admin.post("/api/push/subscriptions", subscription(t, "/admin-phone")), http.StatusCreated)
	ch := a.createChannel(admin, "general", "text")

	a.send(admin, ch, "hello bob")
	fake.waitFor(t, "/bob-phone")
	time.Sleep(100 * time.Millisecond)
	for _, got := range fake.received() {
		if got == "/admin-phone" {
			t.Fatal("the author was told about their own message")
		}
	}

	// Muting the channel stops the pushes.
	wantStatus(t, "mute", bob.put("/api/channels/"+ch.String()+"/notifications", map[string]any{"level": "mute"}), http.StatusNoContent)
	before := len(fake.received())
	a.send(admin, ch, "anybody there?")
	time.Sleep(200 * time.Millisecond)
	if after := len(fake.received()); after != before {
		t.Fatalf("a muted channel still pushed (%d -> %d)", before, after)
	}
}

func TestAnEndedSessionTakesItsPushDeviceWithIt(t *testing.T) {
	a, fake := newPushApp(t, true, nil)
	admin := a.seedAdmin()
	ch := a.createChannel(admin, "general", "text")
	a.register(admin, "bob")
	devices := func(c *client) int {
		var list pushDevices
		c.get("/api/push/subscriptions").decode(t, &list)
		return len(list.Subscriptions)
	}

	// Signing out the registering session ends the device; another session of the account stays.
	first, second := a.login("bob", "member-password-123"), a.login("bob", "member-password-123")
	wantStatus(t, "subscribe", first.post("/api/push/subscriptions", subscription(t, "/signed-out")), http.StatusCreated)
	wantStatus(t, "subscribe 2", second.post("/api/push/subscriptions", subscription(t, "/kept")), http.StatusCreated)
	wantStatus(t, "logout", first.post("/api/auth/logout", nil), http.StatusNoContent)
	if n := devices(second); n != 1 {
		t.Fatalf("%d devices after one session signed out, want the other session's only", n)
	}
	a.send(admin, ch, "hello")
	fake.waitFor(t, "/kept")
	for _, got := range fake.received() {
		if got == "/signed-out" {
			t.Fatal("a signed-out session's device was pushed to")
		}
	}

	// A password change ends every other session, and with them their devices.
	third := a.login("bob", "member-password-123")
	wantStatus(t, "subscribe 3", third.post("/api/push/subscriptions", subscription(t, "/after-reset")), http.StatusCreated)
	wantStatus(t, "revoke", admin.post("/api/admin/users/"+second.user.ID.String()+"/sessions/revoke", nil), http.StatusNoContent)
	time.Sleep(50 * time.Millisecond)
	before := len(fake.received())
	a.send(admin, ch, "again")
	time.Sleep(300 * time.Millisecond)
	if after := len(fake.received()); after != before {
		t.Fatalf("devices of revoked sessions were pushed to (%d -> %d): %v", before, after, fake.received())
	}
}
