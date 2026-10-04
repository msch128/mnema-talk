package httpx

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestDecodeJSONIsStrict(t *testing.T) {
	type payload struct {
		Name string `json:"name"`
	}
	cases := map[string]string{
		"unknown field": `{"name":"a","role":"admin"}`,
		"trailing data": `{"name":"a"}{"name":"b"}`,
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
	if _, err := CleanText("bio", "äöü", 3, false); err != nil {
		t.Errorf("limit counts runes, not bytes: %v", err)
	}
	if _, err := CleanText("bio", "abcd", 3, false); err == nil {
		t.Error("over-long accepted")
	}
}

func TestSanitizeFilename(t *testing.T) {
	cases := map[string]string{
		"../../etc/passwd":    "passwd",
		`C:\Users\x\evil.exe`: "evil.exe",
		".htaccess":           "htaccess",
		"a\r\nb.txt":          "ab.txt",
		`quote"name.png`:      "quotename.png",
		"..":                  "",
		"   ":                 "",
		"normal name (1).jpg": "normal name (1).jpg",
	}
	for in, want := range cases {
		if got := SanitizeFilename(in, 255); got != want {
			t.Errorf("SanitizeFilename(%q) = %q, want %q", in, got, want)
		}
	}
	long := strings.Repeat("ä", 200)
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
