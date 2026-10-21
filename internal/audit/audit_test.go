package audit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSanitizeRedactsSecretsAndBoundsSizes(t *testing.T) {
	long := strings.Repeat("x", 500)
	out := SanitizeDetails(map[string]any{
		"password":   "hunter2",
		"api_token":  "t",
		"Secret":     "s",
		"code":       "invite",
		"session_id": "sid",
		"url":        "https://user:pw@example.com/hook?key=K#frag",
		"long":       long,
		"content":    Content(long),
		"count":      3,
		"nested":     map[string]any{"token_hash": "h", "ok": "fine", "deep": map[string]any{"a": map[string]any{"b": map[string]any{"c": map[string]any{"d": map[string]any{"e": 1}}}}}},
		"list":       make([]any, 80),
	})
	for _, key := range []string{"password", "api_token", "Secret", "code", "session_id"} {
		if out[key] != redacted {
			t.Errorf("key %q not redacted: %v", key, out[key])
		}
	}
	if u, _ := out["url"].(string); !strings.HasPrefix(u, "https://example.com [sha256:") || strings.Contains(u, "hook") || strings.Contains(u, "pw") {
		t.Errorf("url = %v", out["url"])
	}
	if len([]rune(out["long"].(string))) != maxStringRunes {
		t.Error("string not clipped")
	}
	if len([]rune(out["content"].(string))) != 500 {
		t.Error("content must keep its text")
	}
	if out["count"] != 3 || out["nested"].(map[string]any)["token_hash"] != redacted || out["nested"].(map[string]any)["ok"] != "fine" {
		t.Errorf("nested = %v", out["nested"])
	}
	if len(out["list"].([]any)) != maxItems {
		t.Error("list not capped")
	}
	if len(SanitizeDetails(nil)) != 0 {
		t.Error("nil details")
	}
	huge := map[string]any{}
	for i := 0; i < 40; i++ {
		huge[strings.Repeat("k", 10)+string(rune('a'+i%26))+string(rune('A'+i/26))] = Content(strings.Repeat("y", MaxContentRunes))
	}
	if got := SanitizeDetails(huge); got["truncated"] != true {
		t.Error("oversized details kept")
	}
	if got := SanitizeDetails(map[string]any{"bad": func() {}}); got["truncated"] != true {
		t.Error("unmarshalable details kept")
	}
}

func TestMatchFindsRoutesAndParams(t *testing.T) {
	rt, params, ok := Match("PATCH", "/api/admin/roles/abc")
	if !ok || rt.Action != "role.update" || params["id"] != "abc" {
		t.Fatalf("%v %v %v", rt, params, ok)
	}
	if rt, _, ok = Match("PUT", "/api/admin/roles/order"); !ok || rt.Action != "role.reorder" {
		t.Fatalf("order: %v", rt)
	}
	for _, c := range []struct{ m, p string }{
		{"GET", "/api/admin/roles/abc"}, {"PATCH", "/api/other/roles/abc"}, {"PATCH", "/api/admin/roles/"}, {"PATCH", "/api/admin/roles/a/b"},
	} {
		if _, _, ok := Match(c.m, c.p); ok {
			t.Errorf("%s %s matched", c.m, c.p)
		}
	}
	if !mutating("POST") {
		t.Error("POST should be mutating")
	}
	for _, m := range []string{"GET", "HEAD", "OPTIONS"} {
		if mutating(m) {
			t.Errorf("%s should not be mutating", m)
		}
	}
}

func TestDiffAndTime(t *testing.T) {
	got := diff(map[string]any{"a": 1, "b": "x", "c": 2}, map[string]any{"a": 1, "b": "y", "z": 9})
	if len(got) != 1 || got["b"].(map[string]any)["to"] != "y" {
		t.Fatalf("%v", got)
	}
	if _, err := parseTime("2026-01-02T03:04:05Z", false); err != nil {
		t.Fatal(err)
	}
	a, err := parseTime("2026-01-02", false)
	if err != nil {
		t.Fatal(err)
	}
	b, err := parseTime("2026-01-02", true)
	if err != nil {
		t.Fatal(err)
	}
	if b.Sub(a) != 24*time.Hour {
		t.Fatal("end of day")
	}
	if _, err := parseTime("nope", false); err == nil {
		t.Fatal("bad time accepted")
	}
}

func TestNilLogAndSnapshotEdges(t *testing.T) {
	var l *Log
	ctx := context.Background()
	if l.Record(ctx, Entry{}) != nil || l.RecordTx(ctx, nil, Entry{}) != nil {
		t.Fatal("nil log recorded")
	}
	l.RecordOrLog(ctx, Entry{})
	if n, err := l.Prune(ctx, 5); n != 0 || err != nil {
		t.Fatal("nil prune")
	}
	l.RunRetention(ctx, 5, time.Second)
	(&Log{}).RunRetention(ctx, 0, time.Second)
	h := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	if l.Middleware(h) == nil {
		t.Fatal("nil middleware")
	}
	if m, n := Snapshot(ctx, nil, ResRole, "not-a-uuid"); m != nil || n != "" {
		t.Fatal("bad id")
	}
	if m, name := Snapshot(ctx, nil, ResNone, "00000000-0000-0000-0000-000000000000"); m != nil || name != "" {
		t.Fatal("no resource")
	}
	if clip("äöü", 2) != "äö" || clip("a", 2) != "a" {
		t.Fatal("clip")
	}
	c := &capped{max: 4}
	n, err := c.Write([]byte("abcdef"))
	if err != nil || n != 6 || c.buf.String() != "abcd" {
		t.Fatalf("capped: n=%d err=%v buf=%s", n, err, c.buf.String())
	}
	if _, err := c.Write([]byte("zz")); err != nil || c.buf.String() != "abcd" {
		t.Fatal("capped overflow")
	}
}

func TestMiddlewareSkipsReadsAndSkippedRoutes(t *testing.T) {
	l := &Log{} // a nil DB would panic if anything were recorded
	called := 0
	h := l.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called++ }))
	for _, c := range []struct{ m, p string }{{"GET", "/api/admin/roles"}, {"POST", "/api/admin/system/check"}} {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(c.m, c.p, nil))
	}
	if called != 2 {
		t.Fatalf("called %d", called)
	}
	// A failed request is not recorded either.
	h = l.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(403) }))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/api/admin/roles", strings.NewReader(`{}`)))
}
