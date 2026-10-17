package audit

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestURLsKeepOnlySchemeHostAndHash(t *testing.T) {
	out := SanitizeDetails(map[string]any{
		"a": "HTTPS://Hooks.Example.com/services/T0/B0/SECRETTOKEN?x=1",
		"b": "see http://10.0.0.1:8080/path/TOKEN2 for details",
		"c": "http://[bad/TOKEN3",
		"d": "https://hooks.example.com/services/T0/B0/OTHER",
	})
	all := out["a"].(string) + out["b"].(string) + out["c"].(string)
	for _, secret := range []string{"SECRETTOKEN", "TOKEN2", "TOKEN3", "services"} {
		if strings.Contains(all, secret) {
			t.Errorf("%s survived: %s", secret, all)
		}
	}
	if !strings.HasPrefix(out["a"].(string), "https://Hooks.Example.com [sha256:") || !strings.Contains(out["b"].(string), "http://10.0.0.1:8080 [sha256:") {
		t.Errorf("hosts lost: %v", out)
	}
	if out["a"] == out["d"] {
		t.Error("different addresses must hash differently")
	}
}

func TestOversizedDetailsKeepTheChanges(t *testing.T) {
	big := Content(strings.Repeat("y", MaxContentRunes))
	d := map[string]any{"changes": map[string]any{"name": map[string]any{"from": "a", "to": "b"}}}
	for _, k := range []string{"request", "after", "before", "params"} {
		d[k] = map[string]any{"a": big, "b": big, "c": big, "d": big, "e": big, "f": big, "g": big, "h": big, "i": big}
	}
	out := SanitizeDetails(d)
	if out["changes"] == nil || out["request_truncated"] != true {
		t.Fatalf("diff lost or request kept: %v", out)
	}
	// Free-form fields that cannot be trimmed fall back to a marker.
	huge := map[string]any{}
	for _, k := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i"} {
		huge[k] = big
	}
	if SanitizeDetails(huge)["truncated"] != true {
		t.Fatal("untrimmable details kept")
	}
	if size(map[string]any{"f": func() {}}) <= maxDetailBytes {
		t.Fatal("unmarshalable size")
	}
}

func TestMiddlewareMarksBodiesItCannotParse(t *testing.T) {
	var last string
	rec := func(body string, ct string) {
		f := &capturingDB{}
		l := &Log{DB: f}
		h := l.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
		req := httptest.NewRequest("POST", "/api/admin/brand-new", strings.NewReader(body))
		if ct != "" {
			req.Header.Set("Content-Type", ct)
		}
		h.ServeHTTP(httptest.NewRecorder(), req)
		last = f.details
	}
	rec(`{"name":"x"}`, "text/plain")
	if !strings.Contains(last, `"request"`) {
		t.Errorf("text/plain body dropped: %s", last)
	}
	rec(`not json`, "")
	if !strings.Contains(last, "request_unparsed") {
		t.Errorf("unparsed not marked: %s", last)
	}
	rec(`{"a":"`+strings.Repeat("x", maxRequestBody)+`"}`, "application/json")
	if !strings.Contains(last, "request_truncated") {
		t.Errorf("truncation not marked: %s", last)
	}
	rec(`  `, "")
	rec(`{}`, "multipart/form-data; boundary=x")
}

func TestPrivilegedErrorsAndMissing(t *testing.T) {
	l := &Log{DB: &fakeDB{queryErr: errors.New("unused")}, Privileged: func(context.Context) (bool, error) { return false, errors.New("perm down") }}
	if err := l.list(httptest.NewRecorder(), httptest.NewRequest("GET", "/x", nil)); err == nil || err.Error() != "perm down" {
		t.Fatalf("err = %v", err)
	}
	// Without a Privileged hook the query still runs (sensitive rows filtered).
	l = &Log{DB: &fakeDB{queryErr: errors.New("db down")}}
	if err := l.list(httptest.NewRecorder(), httptest.NewRequest("GET", "/x", nil)); err == nil || err.Error() != "db down" {
		t.Fatalf("err = %v", err)
	}
}

// capturingDB remembers the details column of the last insert.
type capturingDB struct{ details string }

func (c *capturingDB) Exec(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
	c.details = string(args[6].([]byte))
	return pgconn.CommandTag{}, nil
}
func (c *capturingDB) Query(context.Context, string, ...any) (pgx.Rows, error) { return nil, nil }
func (c *capturingDB) QueryRow(context.Context, string, ...any) pgx.Row        { return nil }
