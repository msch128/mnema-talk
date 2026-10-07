package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

func TestRecoverPreservesAbortHandlerPanic(t *testing.T) {
	defer func() {
		if got := recover(); got != http.ErrAbortHandler {
			t.Fatalf("abort panic = %v, want http.ErrAbortHandler", got)
		}
	}()
	rec := httptest.NewRecorder()
	Recover(false)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	})).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	t.Fatal("request abort was swallowed")
}

func TestProxyAddressParsingRejectsMalformedPeersAndCIDRs(t *testing.T) {
	trusted, err := ParseCIDRs([]string{"203.0.113.9", "2001:db8::1"})
	if err != nil || len(trusted) != 2 {
		t.Fatalf("bare IPv4/IPv6 allowlist: %v %v", trusted, err)
	}
	if !remoteTrusted("2001:db8::1", trusted) || remoteTrusted("not-a-peer", trusted) {
		t.Fatal("trusted IPv6 or malformed peer policy incorrect")
	}
	if _, err := ParseCIDRs([]string{"203.0.113.0/999"}); err == nil {
		t.Fatal("invalid CIDR accepted")
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "203.0.113.9"
	if got := ClientIP(r); got != r.RemoteAddr {
		t.Fatalf("peer without port: %q", got)
	}
}

func TestCORSIgnoresUnusableAllowlistAndCompletesAllowedPreflight(t *testing.T) {
	h := CORS([]string{"missing-scheme", "https://chat.example.com"})(okHandler)
	r := httptest.NewRequest(http.MethodOptions, "/api/channels", nil)
	r.Header.Set("Origin", "https://chat.example.com")
	r.Header.Set("Access-Control-Request-Method", http.MethodPatch)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent || w.Header().Get("Access-Control-Allow-Origin") != "https://chat.example.com" || !strings.Contains(w.Header().Get("Access-Control-Allow-Methods"), "PATCH") {
		t.Fatalf("allowed preflight: status=%d headers=%v", w.Code, w.Header())
	}
	r.Header.Set("Origin", "missing-scheme")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden || w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("unusable allowlist origin was trusted")
	}
}

func TestHandleSanitizesDownstreamFailureAndKeepsTypedAPIError(t *testing.T) {
	for name, err := range map[string]error{
		"downstream": errors.New("test-only private downstream detail"),
		"typed":      fmt.Errorf("wrapped: %w", ErrForbidden("permission denied")),
	} {
		t.Run(name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/api/channels?never-log-this", nil)
			r = r.WithContext(context.WithValue(r.Context(), requestIDKey, "core-error-test"))
			w := httptest.NewRecorder()
			Handle(func(http.ResponseWriter, *http.Request) error { return err })(w, r)
			want := http.StatusInternalServerError
			if name == "typed" {
				want = http.StatusForbidden
			}
			if w.Code != want || strings.Contains(w.Body.String(), "downstream detail") {
				t.Fatalf("error response status/body: %d %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestQueryLimitClampsAndDefaultsInvalidValues(t *testing.T) {
	for query, want := range map[string]int{"": 20, "?limit=bad": 20, "?limit=0": 20, "?limit=-1": 20, "?limit=10": 10, "?limit=500": 100} {
		if got := QueryLimit(httptest.NewRequest(http.MethodGet, "/"+query, nil), "limit", 20, 100); got != want {
			t.Errorf("QueryLimit(%q) = %d, want %d", query, got, want)
		}
	}
}

func TestLimiterCapsRemainBoundedBeyondEvictionSample(t *testing.T) {
	now := time.Unix(1000, 0)
	rl := NewRateLimiter(2, time.Hour)
	rl.now = func() time.Time { return now }
	rl.maxEntries = 64
	l := NewLoginFailureLimiter(2, time.Minute)
	l.now = func() time.Time { return now }
	l.maxEntries = 64
	for i := 0; i < 256; i++ {
		key := fmt.Sprintf("client-%d", i)
		if ok, _ := rl.Allow(key); !ok {
			t.Fatal("new client incorrectly limited")
		}
		l.RecordFailure(key)
		if len(rl.entries) > 64 || l.Len() > 64 {
			t.Fatal("flooded limiter exceeded its memory cap")
		}
	}
	if ok, _ := rl.Allow("client-255"); !ok {
		t.Fatal("latest client lost first hit")
	}
	if ok, _ := rl.Allow("client-255"); ok {
		t.Fatal("latest client lost its rate-limit history")
	}
	l.RecordFailure("client-255")
	if locked, _ := l.IsLockedOut("client-255"); !locked {
		t.Fatal("latest client lost its login failure history")
	}
}

func TestLoginLockoutTierBounds(t *testing.T) {
	l := NewLoginFailureLimiter(2, time.Minute)
	for n, want := range map[int]time.Duration{0: time.Minute, 1: time.Minute, 2: 5 * time.Minute, 3: 30 * time.Minute, 4: 30 * time.Minute} {
		if got := l.lockoutFor(n); got != want {
			t.Errorf("lockout tier %d = %v, want %v", n, got, want)
		}
	}
}

func TestLoggerOmitsSensitiveQueryAndSkipsHealthRequests(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	h := Logger(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, "created")
	}))
	r := httptest.NewRequest(http.MethodGet, "/api/channels?token=test-only-query-marker", nil)
	r.RemoteAddr = "203.0.113.9:1234"
	r = r.WithContext(context.WithValue(r.Context(), requestIDKey, "logger-core-test"))
	h.ServeHTTP(httptest.NewRecorder(), r)
	var event map[string]any
	if err := json.Unmarshal(output.Bytes(), &event); err != nil {
		t.Fatal(err)
	}
	if event["path"] != "/api/channels" || event["request_id"] != "logger-core-test" || event["status"] != float64(http.StatusCreated) || event["bytes"] != float64(7) || event["ip"] != "203.0.113.9" || strings.Contains(output.String(), "test-only-query-marker") {
		t.Fatalf("unsafe or incomplete request log: %s", output.String())
	}
	output.Reset()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if output.Len() != 0 {
		t.Fatal("health poll generated request log noise")
	}
}

func TestMaxBodyLimitsUnknownLengthAndAllowsEmptyRequests(t *testing.T) {
	h := MaxBody(10)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.ReadAll(r.Body)
		var max *http.MaxBytesError
		if !errors.As(err, &max) {
			t.Fatalf("unknown length oversized body: %v", err)
		}
		WriteError(w, ErrPayloadTooLarge("request body too large"))
	}))
	r := httptest.NewRequest(http.MethodPost, "/api/test", strings.NewReader(strings.Repeat("x", 11)))
	r.ContentLength = -1
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("unknown length limit status: %d", w.Code)
	}
	r = httptest.NewRequest(http.MethodGet, "/", nil)
	r.Body = nil
	w = httptest.NewRecorder()
	MaxBody(10)(okHandler).ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatal("request with no body was rejected")
	}
}

func TestPathUUIDRejectsMissingAndMalformedParameters(t *testing.T) {
	for _, value := range []string{"", "not-a-uuid", "123e4567-e89b-12d3-a456-426614174000"} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		route := chi.NewRouteContext()
		route.URLParams.Add("id", value)
		r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, route))
		id, err := PathUUID(r, "id")
		if value == "123e4567-e89b-12d3-a456-426614174000" {
			if err != nil || id.String() != value {
				t.Fatalf("valid path UUID: id=%v err=%v", id, err)
			}
		} else if api, ok := AsAPIError(err); !ok || api.Status != http.StatusBadRequest {
			t.Fatalf("invalid path UUID %q: %v", value, err)
		}
	}
}

func TestAPIErrorConstructorsPreserveStableStatusAndCode(t *testing.T) {
	for _, tc := range []struct {
		err    *APIError
		status int
		code   string
	}{
		{ErrUnauthorized("test denial"), http.StatusUnauthorized, CodeUnauthorized},
		{ErrNotFound("test denial"), http.StatusNotFound, CodeNotFound},
		{ErrConflict("test denial"), http.StatusConflict, CodeConflict},
		{ErrUnsupportedMediaType("test denial"), http.StatusUnsupportedMediaType, CodeUnsupportedMediaType},
		{ErrUnavailable("test denial"), http.StatusServiceUnavailable, CodeUnavailable},
	} {
		w := httptest.NewRecorder()
		WriteError(w, tc.err)
		var envelope ErrorResponse
		if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		if w.Code != tc.status || envelope.Error.Code != tc.code || envelope.Error.Message != "test denial" || strings.Contains(w.Body.String(), "Status") {
			t.Fatalf("API error contract: status=%d body=%s", w.Code, w.Body.String())
		}
	}
}

func TestFailuresDuringLockoutDoNotRestartOrEscalateIt(t *testing.T) {
	now := time.Unix(1000, 0)
	l := NewLoginFailureLimiter(1, time.Minute)
	l.now = func() time.Time { return now }
	l.RecordFailure("client")
	now = now.Add(30 * time.Second)
	l.RecordFailures("client", 100)
	if locked, retry := l.IsLockedOut("CLIENT"); !locked || retry != 30*time.Second {
		t.Fatalf("active lockout was restarted: locked=%v remaining=%v", locked, retry)
	}
	now = now.Add(30 * time.Second)
	l.RecordFailure("client")
	if locked, retry := l.IsLockedOut("client"); !locked || retry != 5*time.Minute {
		t.Fatalf("new breach used wrong tier: locked=%v remaining=%v", locked, retry)
	}
}
