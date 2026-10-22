package push

import (
	"context"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/ssrf"
)

// received is one request a fake push service saw.
type received struct {
	path   string
	header http.Header
	body   []byte
}

// pushService is a fake push service on a local TLS port. It answers every
// request with status (and Retry-After when set) and records what came in.
type pushService struct {
	*httptest.Server
	mu         sync.Mutex
	got        []received
	status     atomic.Int32
	retryAfter atomic.Value
}

func newPushService(t *testing.T, status int) *pushService {
	t.Helper()
	p := &pushService{}
	p.status.Store(int32(status))
	p.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		p.mu.Lock()
		p.got = append(p.got, received{path: r.URL.Path, header: r.Header.Clone(), body: body})
		p.mu.Unlock()
		if v, _ := p.retryAfter.Load().(string); v != "" {
			w.Header().Set("Retry-After", v)
		}
		w.WriteHeader(int(p.status.Load()))
	}))
	t.Cleanup(p.Close)
	return p
}

func (p *pushService) requests() []received {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]received(nil), p.got...)
}

func testKeys(t *testing.T) Keys {
	t.Helper()
	private, public, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		t.Fatal(err)
	}
	return Keys{Public: public, Private: private}
}

func targetFor(p *pushService, b browser) target {
	return target{id: uuid.New(), endpoint: p.URL + "/push/device-1", p256dh: b.p256dh(), auth: b.secret()}
}

func TestSenderEncryptsForTheBrowserAndSignsWithVAPID(t *testing.T) {
	service := newPushService(t, http.StatusCreated)
	keys := testKeys(t)
	b := newBrowser(t)
	s := &Sender{Keys: keys, Subject: "mailto:ops@example.com", HTTP: service.Client()}

	res := s.Send(context.Background(), targetFor(service, b), []byte(`{"title":"hello"}`),
		Options{TTL: 90 * time.Minute, Urgent: true, Topic: "abc", Timeout: 5 * time.Second})
	if res.Outcome != Delivered {
		t.Fatalf("outcome %+v", res)
	}
	reqs := service.requests()
	if len(reqs) != 1 {
		t.Fatalf("%d requests", len(reqs))
	}
	h := reqs[0].header
	for name, want := range map[string]string{
		"Content-Encoding": "aes128gcm", "Ttl": "5400", "Urgency": "high", "Topic": "abc",
		"Content-Type": "application/octet-stream",
	} {
		if got := h.Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	if got := string(b.open(t, reqs[0].body)); got != `{"title":"hello"}` {
		t.Fatalf("browser reads %q", got)
	}
	if strings.Contains(string(reqs[0].body), "hello") {
		t.Fatal("the push service can read the payload")
	}
	checkVAPID(t, h.Get("Authorization"), keys, service.URL, "mailto:ops@example.com")
}

// checkVAPID verifies the RFC 8292 header: the JWT signature with the server's
// public key, the audience (origin of the push service), the contact and an
// expiry within 24 hours, and the public key in k=.
func checkVAPID(t *testing.T, header string, keys Keys, endpointOrigin, subject string) {
	t.Helper()
	token, key, ok := strings.Cut(strings.TrimPrefix(header, "vapid t="), ", k=")
	if !strings.HasPrefix(header, "vapid t=") || !ok {
		t.Fatalf("Authorization %q is not a vapid header", header)
	}
	if key != keys.Public {
		t.Fatalf("k = %q, want the server's public key", key)
	}
	point := b64(t, keys.Public)
	pub, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), point)
	if err != nil {
		t.Fatal(err)
	}
	claims := jwt.MapClaims{}
	parsed, err := jwt.ParseWithClaims(token, claims, func(*jwt.Token) (any, error) { return pub, nil },
		jwt.WithValidMethods([]string{"ES256"}), jwt.WithExpirationRequired())
	if err != nil || !parsed.Valid {
		t.Fatalf("JWT does not verify: %v", err)
	}
	if aud, _ := claims["aud"].(string); aud != endpointOrigin {
		t.Errorf("aud = %q, want %q", aud, endpointOrigin)
	}
	if sub, _ := claims["sub"].(string); sub != subject {
		t.Errorf("sub = %q, want %q", sub, subject)
	}
	exp, _ := claims.GetExpirationTime()
	if remaining := time.Until(exp.Time); remaining <= 0 || remaining > 24*time.Hour {
		t.Errorf("exp in %s, RFC 8292 allows at most 24 hours", remaining)
	}
}

func TestSenderUsesAnHTTPSContactAsIs(t *testing.T) {
	service := newPushService(t, http.StatusCreated)
	keys := testKeys(t)
	s := &Sender{Keys: keys, Subject: "https://chat.example.com", HTTP: service.Client()}
	if res := s.Send(context.Background(), targetFor(service, newBrowser(t)), []byte("x"), Options{TTL: time.Minute}); res.Outcome != Delivered {
		t.Fatalf("outcome %+v", res)
	}
	checkVAPID(t, service.requests()[0].header.Get("Authorization"), keys, service.URL, "https://chat.example.com")
}

func TestSenderClassifiesAnswers(t *testing.T) {
	cases := []struct {
		status int
		retry  string
		want   Outcome
		wait   time.Duration
	}{
		{http.StatusCreated, "", Delivered, 0},
		{http.StatusAccepted, "", Delivered, 0},
		{http.StatusNotFound, "", Gone, 0},
		{http.StatusGone, "", Gone, 0},
		{http.StatusTooManyRequests, "7", Retry, 7 * time.Second},
		{http.StatusServiceUnavailable, "3600", Retry, maxRetryWait},
		{http.StatusBadGateway, "soon", Retry, 0},
		{http.StatusBadRequest, "", Rejected, 0},
		{http.StatusForbidden, "", Rejected, 0},
		{http.StatusRequestEntityTooLarge, "", Rejected, 0},
	}
	for _, c := range cases {
		service := newPushService(t, c.status)
		service.retryAfter.Store(c.retry)
		s := &Sender{Keys: testKeys(t), Subject: "mailto:a@b.c", HTTP: service.Client()}
		res := s.Send(context.Background(), targetFor(service, newBrowser(t)), []byte("x"), Options{TTL: time.Minute})
		if res.Outcome != c.want || res.RetryAfter != c.wait {
			t.Errorf("status %d: %+v, want outcome %s wait %s", c.status, res, c.want, c.wait)
		}
	}
}

func TestSenderRefusesAddressesOnThePrivateNetwork(t *testing.T) {
	service := newPushService(t, http.StatusCreated)
	// The server is on 127.0.0.1: the guarded client must not connect there.
	guarded := ssrf.NewClient(ssrf.NewDialer(&ssrf.Policy{}), 2*time.Second)
	s := &Sender{Keys: testKeys(t), Subject: "mailto:a@b.c", HTTP: guarded}
	res := s.Send(context.Background(), targetFor(service, newBrowser(t)), []byte("x"), Options{TTL: time.Minute})
	if res.Outcome != Failed || res.Reason != "blocked_address" {
		t.Fatalf("result %+v, want a refusal of the address", res)
	}
	if n := len(service.requests()); n != 0 {
		t.Fatalf("the push service received %d requests", n)
	}
}

func TestSenderReportsLowLevelFailuresByCode(t *testing.T) {
	service := newPushService(t, http.StatusCreated)
	b := newBrowser(t)
	s := &Sender{Keys: testKeys(t), Subject: "mailto:a@b.c", HTTP: service.Client()}

	t.Run("timeout", func(t *testing.T) {
		slow := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-r.Context().Done():
			case <-time.After(time.Second):
			}
		}))
		defer slow.Close()
		s := &Sender{Keys: s.Keys, Subject: s.Subject, HTTP: slow.Client()}
		res := s.Send(context.Background(), target{endpoint: slow.URL, p256dh: b.p256dh(), auth: b.secret()}, []byte("x"), Options{TTL: time.Minute, Timeout: 50 * time.Millisecond})
		if res.Outcome != Failed || res.Reason != "timeout" {
			t.Fatalf("%+v", res)
		}
	})
	t.Run("untrusted certificate", func(t *testing.T) {
		s := &Sender{Keys: s.Keys, Subject: s.Subject, HTTP: &http.Client{}}
		res := s.Send(context.Background(), targetFor(service, b), []byte("x"), Options{TTL: time.Minute})
		if res.Outcome != Failed || res.Reason != "tls_error" {
			t.Fatalf("%+v", res)
		}
	})
	t.Run("a payload that does not fit one record", func(t *testing.T) {
		res := s.Send(context.Background(), targetFor(service, b), make([]byte, 5000), Options{TTL: time.Minute})
		if res.Outcome != Failed || res.Reason != "payload_too_large" {
			t.Fatalf("%+v", res)
		}
	})
	t.Run("broken subscription keys", func(t *testing.T) {
		res := s.Send(context.Background(), target{endpoint: service.URL, p256dh: "%%%", auth: "%%%"}, []byte("x"), Options{TTL: time.Minute})
		if res.Outcome != Failed {
			t.Fatalf("%+v", res)
		}
	})
	t.Run("an unreachable server", func(t *testing.T) {
		dead := httptest.NewTLSServer(http.NotFoundHandler())
		client, endpoint := dead.Client(), dead.URL
		dead.Close()
		s := &Sender{Keys: s.Keys, Subject: s.Subject, HTTP: client}
		res := s.Send(context.Background(), target{endpoint: endpoint, p256dh: b.p256dh(), auth: b.secret()}, []byte("x"), Options{TTL: time.Minute})
		if res.Outcome != Failed || res.Reason == "" {
			t.Fatalf("%+v", res)
		}
	})
}

func TestBrowserKeyGeneratedForTestsIsValid(t *testing.T) {
	b := newBrowser(t)
	if _, err := ecdh.P256().NewPublicKey(b.key.PublicKey().Bytes()); err != nil {
		t.Fatal(err)
	}
	if p, a, err := ValidateKeys(b.p256dh(), b.secret()); err != nil || p != b.p256dh() || a != b.secret() {
		t.Fatalf("%v %s %s", err, p, a)
	}
}
