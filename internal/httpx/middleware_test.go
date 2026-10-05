package httpx

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

var okHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

func TestCheckSameOrigin(t *testing.T) {
	allowed := []string{"https://chat.example.com"}
	cases := []struct {
		name, origin, referer string
		want                  bool
	}{
		{"exact origin", "https://chat.example.com", "", true},
		{"case-insensitive host", "https://CHAT.example.com", "", true},
		{"referer fallback", "", "https://chat.example.com/some/page?x=1", true},
		{"foreign origin", "https://evil.example", "", false},
		{"scheme mismatch", "http://chat.example.com", "", false},
		{"subdomain is foreign", "https://a.chat.example.com", "", false},
		{"missing both", "", "", false},
		{"null origin", "null", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/api/x", nil)
			if tc.origin != "" {
				r.Header.Set("Origin", tc.origin)
			}
			if tc.referer != "" {
				r.Header.Set("Referer", tc.referer)
			}
			if got := CheckSameOrigin(r, allowed); got != tc.want {
				t.Fatalf("CheckSameOrigin = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCSRFBlocksUnsafeCrossOriginOnly(t *testing.T) {
	h := CSRF([]string{"https://chat.example.com"})(okHandler)

	get := httptest.NewRequest(http.MethodGet, "/api/x", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, get)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET without origin: got %d, want 200", rec.Code)
	}

	post := httptest.NewRequest(http.MethodPost, "/api/x", nil)
	post.Header.Set("Origin", "https://evil.example")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, post)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("cross-origin POST: got %d, want 403", rec.Code)
	}

	post = httptest.NewRequest(http.MethodDelete, "/api/x", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, post)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("DELETE without origin: got %d, want 403", rec.Code)
	}

	post = httptest.NewRequest(http.MethodPost, "/api/x", nil)
	post.Header.Set("Origin", "https://chat.example.com")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, post)
	if rec.Code != http.StatusOK {
		t.Fatalf("same-origin POST: got %d, want 200", rec.Code)
	}
}

func TestCORS(t *testing.T) {
	h := CORS([]string{"https://chat.example.com/"})(okHandler)

	r := httptest.NewRequest(http.MethodGet, "/api/x", nil)
	r.Header.Set("Origin", "https://chat.example.com")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://chat.example.com" {
		t.Fatalf("allowed origin echoed as %q", got)
	}

	r = httptest.NewRequest(http.MethodGet, "/api/x", nil)
	r.Header.Set("Origin", "https://evil.example")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("foreign origin must not be allowed, got %q", got)
	}

	pre := httptest.NewRequest(http.MethodOptions, "/api/x", nil)
	pre.Header.Set("Origin", "https://evil.example")
	pre.Header.Set("Access-Control-Request-Method", "POST")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, pre)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("foreign preflight: got %d, want 403", rec.Code)
	}
}

func TestSecurityHeaders(t *testing.T) {
	trusted, _ := ParseCIDRs([]string{"10.0.0.0/8"})
	h := SecurityHeaders(trusted, false)(okHandler)

	r := httptest.NewRequest(http.MethodGet, "/api/channels", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	for header, want := range map[string]string{
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "DENY",
		"Cache-Control":           "no-store",
		"Content-Security-Policy": "default-src 'none'; frame-ancestors 'none'",
	} {
		if got := rec.Header().Get(header); got != want {
			t.Errorf("API %s = %q, want %q", header, got, want)
		}
	}
	if rec.Header().Get("Strict-Transport-Security") != "" {
		t.Error("no HSTS expected for plain http in development")
	}

	r = httptest.NewRequest(http.MethodGet, "/", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if got := rec.Header().Get("Content-Security-Policy"); got != SPAContentSecurityPolicy {
		t.Errorf("SPA CSP = %q", got)
	}
	if strings.Contains(SPAContentSecurityPolicy, "'unsafe-eval'") || strings.Contains(SPAContentSecurityPolicy, "script-src 'self' 'unsafe-inline'") {
		t.Error("SPA CSP must not allow inline or eval scripts")
	}

	// A spoofed X-Forwarded-Proto from an untrusted peer must not earn HSTS.
	r = httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "203.0.113.9:1234"
	r.Header.Set("X-Forwarded-Proto", "https")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Header().Get("Strict-Transport-Security") != "" {
		t.Error("HSTS granted to untrusted X-Forwarded-Proto")
	}

	r = httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "10.1.2.3:1234"
	r.Header.Set("X-Forwarded-Proto", "https")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Header().Get("Strict-Transport-Security") == "" {
		t.Error("HSTS expected behind a trusted TLS proxy")
	}

	rec = httptest.NewRecorder()
	SecurityHeaders(nil, true)(okHandler).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Header().Get("Strict-Transport-Security") == "" {
		t.Error("HSTS expected in production")
	}
}

func TestClientIPIgnoresSpoofedForwardedFor(t *testing.T) {
	trusted, _ := ParseCIDRs([]string{"172.16.0.0/12"})
	var got string
	h := ClientIPMiddleware(trusted)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got = ClientIP(r) }))

	cases := []struct {
		name, remote, xff, want string
	}{
		{"untrusted peer ignores header", "203.0.113.5:999", "1.2.3.4", "203.0.113.5"},
		{"trusted proxy uses client hop", "172.21.0.1:999", "198.51.100.7", "198.51.100.7"},
		{"client-supplied prefix is ignored", "172.21.0.1:999", "1.1.1.1, 198.51.100.7", "198.51.100.7"},
		{"chain of trusted proxies", "172.21.0.1:999", "198.51.100.7, 172.20.0.5", "198.51.100.7"},
		{"garbage header falls back to peer", "172.21.0.1:999", "not-an-ip", "172.21.0.1"},
		{"loopback is always trusted", "127.0.0.1:999", "198.51.100.8", "198.51.100.8"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = tc.remote
			r.Header.Set("X-Forwarded-For", tc.xff)
			h.ServeHTTP(httptest.NewRecorder(), r)
			if got != tc.want {
				t.Fatalf("ClientIP = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMaxBodyRejectsDeclaredOversize(t *testing.T) {
	h := MaxBody(10)(okHandler)
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(strings.Repeat("x", 11)))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("got %d, want 413", rec.Code)
	}
}

func TestRequestIDRejectsUnsafeInput(t *testing.T) {
	h := RequestID(okHandler)
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Request-Id", "evil\r\nSet-Cookie: x=1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if id := rec.Header().Get("X-Request-Id"); id == "" || strings.ContainsAny(id, "\r\n ") {
		t.Fatalf("unsafe request id reflected: %q", id)
	}

	r.Header.Set("X-Request-Id", "abc-123")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if id := rec.Header().Get("X-Request-Id"); id != "abc-123" {
		t.Fatalf("well-formed id not kept: %q", id)
	}
}

func TestRecoverReturnsJSON500(t *testing.T) {
	h := Recover(true)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { panic("boom") }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), CodeInternalError) {
		t.Fatalf("got %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "boom") {
		t.Fatal("panic value leaked to the client")
	}
}
