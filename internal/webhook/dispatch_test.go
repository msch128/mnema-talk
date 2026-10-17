package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/ssrf"
)

func TestSignMatchesTheDocumentedScheme(t *testing.T) {
	at := time.Unix(1700000000, 0)
	body := []byte(`{"a":1}`)
	mac := hmac.New(sha256.New, []byte("whsec_k"))
	mac.Write([]byte("1700000000." + string(body)))
	want := "t=1700000000,v1=" + hex.EncodeToString(mac.Sum(nil))
	if got := Sign("whsec_k", at, body); got != want {
		t.Fatalf("Sign = %q, want %q", got, want)
	}
	if Sign("whsec_other", at, body) == want || Sign("whsec_k", at.Add(time.Second), body) == want || Sign("whsec_k", at, []byte(`{"a":2}`)) == want {
		t.Fatal("signature ignores secret, time or body")
	}
}

func TestTranslate(t *testing.T) {
	ch := uuid.New()
	human := chat.Message{ID: uuid.New(), ChannelID: ch, Content: "hi"}
	bot := chat.Message{ID: uuid.New(), ChannelID: ch, IsBot: true}
	cases := []struct {
		name    string
		typ     string
		payload any
		want    string
	}{
		{"create value", "message_create", human, EventMessageCreate},
		{"create pointer", "message_create", &human, EventMessageCreate},
		{"update value", "message_update", human, EventMessageUpdate},
		{"update by a bot is delivered", "message_update", &bot, EventMessageUpdate},
		{"create by a bot is not", "message_create", &bot, ""},
		{"create with another payload", "message_create", "text", ""},
		{"delete", "message_delete", map[string]any{"id": 1}, EventMessageDelete},
		{"reactions", "message_reaction", chat.ReactionResult{}, EventReactionsUpdate},
		{"voice", "voice_state_update", map[string]any{"action": "join"}, EventVoiceStateUpdate},
		{"a new member is not offered", "member_joined", map[string]any{"id": 1}, ""},
		{"typing", "typing", nil, ""},
		{"presence", "presence_update", nil, ""},
	}
	for _, tc := range cases {
		name, data, ok := translate(tc.typ, tc.payload)
		if (tc.want == "") == ok || name != tc.want || (ok && data == nil) {
			t.Errorf("%s: %q %v %v", tc.name, name, data, ok)
		}
	}
}

func TestClassifyAndRetriable(t *testing.T) {
	deadline, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	cases := []struct {
		err    error
		status int
		want   string
		retry  bool
	}{
		{nil, 200, "http_200", false},
		{nil, 302, "redirect_not_followed", false},
		{nil, 404, "http_404", false},
		{nil, 408, "http_408", true},
		{nil, 429, "http_429", true},
		{nil, 503, "http_503", true},
		{ssrf.ErrBlocked, 0, "blocked_address", false},
		{ssrf.ErrResolve, 0, "dns_error", true},
		{ssrf.ErrConnect, 0, "connect_error", true},
		{ssrf.ErrBadURL, 0, "invalid_url", false},
		{deadline.Err(), 0, "timeout", true},
		{timeoutErr{}, 0, "timeout", true},
		{errors.New("tls: handshake failure"), 0, "tls_error", true},
		{errors.New("x509: certificate has expired"), 0, "tls_error", true},
		{errors.New("something with 10.0.0.5 in it"), 0, "request_error", true},
	}
	for _, tc := range cases {
		got := classify(tc.err, tc.status)
		if got != tc.want {
			t.Errorf("classify(%v, %d) = %q, want %q", tc.err, tc.status, got, tc.want)
		}
		d := Delivery{Error: &got}
		if tc.status != 0 {
			s := tc.status
			d.StatusCode = &s
		}
		if tc.err == nil && tc.status < 300 {
			continue
		}
		if retriable(d) != tc.retry {
			t.Errorf("retriable(%q, %d) = %v", got, tc.status, !tc.retry)
		}
	}
	if retriable(Delivery{Success: true}) {
		t.Error("a success is not retried")
	}
}

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

var _ net.Error = timeoutErr{}

func TestBackoff(t *testing.T) {
	d := NewDispatcher(nil, nil, "t")
	for i := 1; i <= len(defaultBackoff); i++ {
		w := d.backoff(i)
		base := defaultBackoff[i-1]
		if w < base-base/10 || w > base+base/10+1 {
			t.Errorf("attempt %d waits %v, base %v", i, w, base)
		}
	}
	if d.backoff(len(defaultBackoff)+1) >= 0 {
		t.Error("no end to the retries")
	}
	d.SetBackoff([]time.Duration{time.Millisecond})
	if w := d.backoff(1); w <= 0 || w > 2*time.Millisecond {
		t.Errorf("custom backoff %v", w)
	}
	if d.backoff(2) >= 0 {
		t.Error("custom list not honoured")
	}
}

func TestObserveRoutesByChannelAndEvent(t *testing.T) {
	d := NewDispatcher(nil, nil, "t")
	chA, chB := uuid.New(), uuid.New()
	a, b, both, off := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	routes := []route{
		{id: a, channel: chA, events: []string{EventMessageCreate}},
		{id: b, channel: chB, events: []string{EventMessageCreate, EventMessageDelete}},
		{id: both, channel: chA, events: []string{EventMessageCreate, EventMessageDelete}},
		{id: off, channel: chA, events: []string{EventMessageUpdate}},
	}
	d.routes.Store(&routes)
	msg := chat.Message{ID: uuid.New(), ChannelID: chA}

	got := func() map[uuid.UUID]bool {
		out := map[uuid.UUID]bool{}
		for {
			select {
			case j := <-d.queue:
				out[j.webhook] = true
				if j.attempt != 1 || j.event.Type == "" || j.event.ID == uuid.Nil || j.event.Channel == uuid.Nil {
					t.Errorf("job %+v", j)
				}
			default:
				return out
			}
		}
	}
	d.Observe(chA, "message_create", &msg)
	if g := got(); len(g) != 2 || !g[a] || !g[both] {
		t.Fatalf("message in A reached %v", g)
	}
	d.Observe(chB, "message_delete", map[string]any{"id": 1})
	if g := got(); len(g) != 1 || !g[b] {
		t.Fatalf("delete in B reached %v", g)
	}
	d.Observe(chA, "typing", nil)
	d.Observe(chA, "message_reaction", chat.ReactionResult{})
	// Events to everybody (no channel) belong to no webhook.
	d.Observe(uuid.Nil, "message_create", &msg)
	d.Observe(uuid.Nil, "member_joined", map[string]any{"id": 1})
	if g := got(); len(g) != 0 {
		t.Fatalf("unsubscribed events reached %v", g)
	}
}

func TestPostSignsAndBoundsTheExchange(t *testing.T) {
	var seen http.Header
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Clone()
		body = make([]byte, 1<<10)
		n, _ := r.Body.Read(body)
		body = body[:n]
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write(make([]byte, 2*readLimit)) // far more than is read
	}))
	defer srv.Close()
	allow, _ := ssrf.ParseAllow("127.0.0.0/8")
	d := NewDispatcher(nil, ssrf.NewClient(ssrf.NewDialer(&ssrf.Policy{Allow: allow}), time.Second), "9.9")
	rec := &outgoingRec{Outgoing: Outgoing{ID: uuid.New(), URL: srv.URL}, secret: "whsec_s"}
	ev := Event{ID: uuid.New(), Type: EventMessageCreate, Time: time.Now(), Data: []byte(`{}`)}

	del, retryAfter := d.attempt(context.Background(), rec, ev, 3)
	if del.Success || del.Attempt != 3 || del.StatusCode == nil || *del.StatusCode != 503 || del.Error == nil || *del.Error != "http_503" || retryAfter != 7*time.Second {
		t.Fatalf("delivery %+v, retry after %v", del, retryAfter)
	}
	if seen.Get("X-Mnema-Event") != "MESSAGE_CREATE" || seen.Get("User-Agent") != "MnemaTalk-Webhook/9.9" || seen.Get("X-Mnema-Delivery") != ev.ID.String() {
		t.Fatalf("headers %v", seen)
	}
	ts, _ := strconv.ParseInt(seen.Get("X-Mnema-Signature")[2:12], 10, 64)
	if Sign("whsec_s", time.Unix(ts, 0), body) != seen.Get("X-Mnema-Signature") {
		t.Fatalf("signature does not verify")
	}
	var env Envelope
	if err := json.Unmarshal(body, &env); err != nil || env.T != "MESSAGE_CREATE" || env.WebhookID != rec.ID.String() || env.ChannelID != nil {
		t.Fatalf("envelope %s: %+v %v", body, env, err)
	}

	// An address that cannot be requested is a delivery failure, not a panic.
	bad := &outgoingRec{Outgoing: Outgoing{ID: uuid.New(), URL: "http://bad host/"}}
	del, _ = d.attempt(context.Background(), bad, ev, 1)
	if del.Success || del.Error == nil || *del.Error != "invalid_url" {
		t.Fatalf("bad url: %+v", del)
	}
	// With a channel the envelope names it.
	ev.Channel = uuid.New()
	if e := newEnvelope(rec, ev); e.ChannelID == nil || *e.ChannelID != ev.Channel.String() {
		t.Fatalf("envelope %+v", e)
	}
}

func TestValidEvents(t *testing.T) {
	got, err := validEvents([]string{"MESSAGE_DELETE", "MESSAGE_CREATE", "MESSAGE_CREATE"})
	if err != nil || len(got) != 2 || got[0] != "MESSAGE_CREATE" {
		t.Fatalf("%v %v", got, err)
	}
	for _, bad := range [][]string{nil, {}, {"PING"}, {"NOPE"}, {"MEMBER_ADD"}} {
		if _, err := validEvents(bad); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
	if s := newSecret(); len(s) < 40 || s == newSecret() {
		t.Errorf("secret %q", s)
	}
}
