package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
)

func TestDecodeJSONIsStrict(t *testing.T) {
	type payload struct {
		Name string `json:"name"`
	}
	cases := map[string]string{
		"unknown field": `{"name":"a","role":"admin"}`,
		"trailing data": `{"name":"a"}{"name":"b"}`,
		"stray brace":   `{"name":"a"}}`,
		"stray bracket": `{"name":"a"}]`,
		"not json":      `name=a`,
		"empty":         ``,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			var p payload
			err := DecodeJSON(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body)), &p)
			ae, ok := AsAPIError(err)
			if !ok || ae.Status != http.StatusBadRequest {
				t.Fatalf("expected 400 APIError, got %v", err)
			}
		})
	}

	var p payload
	if err := DecodeJSON(httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"ok"}`)), &p); err != nil || p.Name != "ok" {
		t.Fatalf("valid body: %v %+v", err, p)
	}
}

func TestDecodeJSONTooLarge(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"`+strings.Repeat("x", 100)+`"}`))
	r.Body = http.MaxBytesReader(httptest.NewRecorder(), r.Body, 20)
	var p struct{ Name string }
	ae, ok := AsAPIError(DecodeJSON(r, &p))
	if !ok || ae.Status != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %v", ae)
	}
}

func TestCleanText(t *testing.T) {
	if _, err := CleanText("bio", "  ", 10, true); err == nil {
		t.Error("required blank accepted")
	}
	if got, err := CleanText("bio", "  hi  ", 10, true); err != nil || got != "hi" {
		t.Errorf("trim: %q %v", got, err)
	}
	if _, err := CleanText("bio", "a\x00b", 10, false); err == nil {
		t.Error("NUL byte accepted")
	}
	if _, err := CleanText("bio", "\u00e4\u00f6\u00fc", 3, false); err != nil {
		t.Errorf("limit counts runes, not bytes: %v", err)
	}
	if _, err := CleanText("bio", "abcd", 3, false); err == nil {
		t.Error("over-long accepted")
	}
}

func TestSanitizeFilename(t *testing.T) {
	cases := map[string]string{
		"../../etc/passwd":          "passwd",
		`C:\Users\x\evil.exe`:       "evil.exe",
		".htaccess":                 "htaccess",
		"a\r\nb.txt":                "ab.txt",
		`quote"name.png`:            "quotename.png",
		"..":                        "",
		"   ":                       "",
		"normal name (1).jpg":       "normal name (1).jpg",
		"invoice\u202efdp.exe":      "invoicefdp.exe", // right-to-left override
		"a\u200bb\u2066c\ufeff.txt": "abc.txt",        // zero-width and isolate controls
		"x\u0085y\u2028z.txt":       "xyz.txt",        // C1 control, line separator
	}
	for in, want := range cases {
		if got := SanitizeFilename(in, 255); got != want {
			t.Errorf("SanitizeFilename(%q) = %q, want %q", in, got, want)
		}
	}
	long := strings.Repeat("\u00e4", 200)
	if got := SanitizeFilename(long, 255); len(got) > 255 || !utf8.ValidString(got) {
		t.Errorf("truncation broke UTF-8 or limit: len=%d", len(got))
	}
}

func TestWriteErrorHidesInternalErrors(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteError(rec, errors.New(`pq: duplicate key value violates unique constraint "users_pkey"`))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "users_pkey") {
		t.Fatal("internal error detail leaked")
	}
}

func TestRateLimiter(t *testing.T) {
	now := time.Unix(0, 0)
	rl := NewRateLimiter(2, time.Minute)
	rl.now = func() time.Time { return now }

	for i := 0; i < 2; i++ {
		if ok, _ := rl.Allow("k"); !ok {
			t.Fatalf("hit %d rejected", i)
		}
	}
	if ok, retry := rl.Allow("k"); ok || retry <= 0 {
		t.Fatalf("third hit allowed (retry %v)", retry)
	}
	if ok, _ := rl.Allow("other"); !ok {
		t.Fatal("keys must be independent")
	}
	now = now.Add(time.Minute)
	if ok, _ := rl.Allow("k"); !ok {
		t.Fatal("window did not reset")
	}
}

func TestLoginFailureLimiterEscalates(t *testing.T) {
	now := time.Unix(0, 0)
	l := NewLoginFailureLimiter(3, time.Minute)
	l.now = func() time.Time { return now }

	fail := func(n int) {
		for i := 0; i < n; i++ {
			l.RecordFailure("Herzog")
		}
	}
	fail(2)
	if locked, _ := l.IsLockedOut("herzog"); locked {
		t.Fatal("locked before threshold")
	}
	fail(1)
	locked, remaining := l.IsLockedOut("HERZOG")
	if !locked || remaining != time.Minute {
		t.Fatalf("first lockout: locked=%v remaining=%v", locked, remaining)
	}

	now = now.Add(time.Minute)
	if locked, _ := l.IsLockedOut("herzog"); locked {
		t.Fatal("first lockout did not expire")
	}
	fail(3)
	if _, remaining := l.IsLockedOut("herzog"); remaining != 5*time.Minute {
		t.Fatalf("second lockout should escalate to 5m, got %v", remaining)
	}

	l.ResetFailures("herzog")
	if locked, _ := l.IsLockedOut("herzog"); locked {
		t.Fatal("reset did not clear lockout")
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

func TestCleanNameStripsInvisibleSpoofing(t *testing.T) {
	got, err := CleanName("username", "\u202eadmin\u200b \u2066x", 32, true)
	if err != nil || got != "admin x" {
		t.Fatalf("got %q, %v", got, err)
	}
	if got, err := CleanName("name", "Zoë 🎉", 32, true); err != nil || got != "Zoë 🎉" {
		t.Fatalf("ordinary name changed: %q %v", got, err)
	}
	// A name made only of invisible characters is empty, hence refused.
	if _, err := CleanName("username", "\u202e\u200b", 32, true); err == nil {
		t.Fatal("invisible-only name accepted")
	}
	if _, err := CleanName("name", "a\x00b", 32, true); err == nil {
		t.Fatal("NUL accepted")
	}
}

func TestCleanNameKeepsJoinersThatScriptsAndEmojiNeed(t *testing.T) {
	for name, in := range map[string]string{
		"family emoji": "👨\u200d👩\u200d👧",
		"rainbow flag": "🏳\ufe0f\u200d🌈",
		"modifier":     "👩🏽\u200d🚀",
		"persian word": "می\u200cخواهم",
		"hindi word":   "क\u094d\u200cष",
	} {
		if got, err := CleanName("name", in, 32, true); err != nil || got != in {
			t.Errorf("%s: got %q, %v; want it unchanged", name, got, err)
		}
	}
}

func TestCleanNameStripsJoinersThatOnlyDisguise(t *testing.T) {
	for in, want := range map[string]string{
		"Ad\u200dmin":       "Admin", // Latin neighbours
		"Own\u200cer":       "Owner",
		"\u200dabc\u200d":   "abc", // leading and trailing
		"a\u200d\u200db":    "ab",  // repeated
		"a\u200d\u202eb":    "ab",  // next to a bidi control
		"\u202eadmin\u200d": "admin",
		"👨\u200dx":          "👨x", // emoji then letter
		"m\u200cی":          "mی", // ZWNJ between Latin and Arabic
		"👨\u200d\u2066👩":    "👨👩", // a bidi isolate splits the sequence
		"1\u200d2":          "12",
	} {
		if got, err := CleanName("name", in, 32, true); err != nil || got != want {
			t.Errorf("%q: got %q, %v; want %q", in, got, err, want)
		}
	}
}

func TestNameSkeletonIgnoresInvisiblesAndCompatibilityForms(t *testing.T) {
	for _, in := range []string{"Everyone", "every\u200done", "ＥＶＥＲＹＯＮＥ", "E\u200bveryone"} {
		if got := NameSkeleton(in); got != "everyone" {
			t.Errorf("%q: skeleton %q", in, got)
		}
	}
}
