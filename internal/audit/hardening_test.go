package audit

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
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

func TestViewerErrorsAndMissing(t *testing.T) {
	l := &Log{DB: &fakeDB{queryErr: errors.New("unused")}, Viewer: func(context.Context) (Viewer, error) { return Viewer{}, errors.New("perm down") }}
	if err := l.list(httptest.NewRecorder(), httptest.NewRequest("GET", "/x", nil)); err == nil || err.Error() != "perm down" {
		t.Fatalf("err = %v", err)
	}
	// Without a Viewer hook the query still runs (sensitive rows filtered).
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

type brokenBody struct {
	data []byte
	err  error
}

func (b *brokenBody) Read(p []byte) (int, error) {
	if len(b.data) > 0 {
		n := copy(p, b.data)
		b.data = b.data[n:]
		return n, nil
	}
	return 0, b.err
}

func (*brokenBody) Close() error { return nil }

func TestPeekRequestKeepsTheBodyAfterAReadError(t *testing.T) {
	boom := errors.New("connection reset")
	r := httptest.NewRequest(http.MethodPost, "/api/x", nil)
	r.Body = &brokenBody{data: []byte(`{"name":"a`), err: boom}
	req, mark := peekRequest(r, false)
	if req != nil || mark != "request_unparsed" {
		t.Fatalf("got %v %q", req, mark)
	}
	got, err := io.ReadAll(r.Body)
	if string(got) != `{"name":"a` || !errors.Is(err, boom) {
		t.Fatalf("handler sees %q, %v; want the bytes read so far and the error", got, err)
	}
}

func TestTypedValuesGetTheSameRules(t *testing.T) {
	id := uuid.New()
	out := SanitizeDetails(map[string]any{
		"attachments": []map[string]any{{"name": "see https://h.example/p/TOKEN", "token": "x"}},
		"params":      map[string]string{"api_token": "SECRET", "id": "7"},
		"reply_ids":   []uuid.UUID{id},
	})
	b, _ := json.Marshal(out)
	if strings.Contains(string(b), "TOKEN\"") || strings.Contains(string(b), "SECRET") || strings.Contains(string(b), "/p/") {
		t.Fatalf("typed values bypassed sanitising: %s", b)
	}
	if !strings.Contains(string(b), id.String()) {
		t.Fatalf("uuid lost: %s", b)
	}
}

func TestHugeEditHistoryKeepsTheRestOfTheEntry(t *testing.T) {
	versions := make([]map[string]any, 50)
	for i := range versions {
		versions[i] = map[string]any{"content": Content(strings.Repeat("z", MaxContentRunes))}
	}
	out := SanitizeDetails(map[string]any{
		"content": Content("deleted text"), "channel_name": "general", "edit_versions": versions,
	})
	if out["truncated"] != nil || out["content"] != "deleted text" || out["channel_name"] != "general" {
		t.Fatalf("entry discarded: %v", out)
	}
	if out["edit_versions_truncated"] != true || len(out["edit_versions"].([]any)) == 0 || size(out) > maxDetailBytes {
		t.Fatalf("edit versions not shrunk: %d", size(out))
	}
}

func TestTakeCreatedTargetKeepsAnExistingName(t *testing.T) {
	e := Entry{TargetName: "instance settings"}
	takeCreatedTarget(&e, []byte(`{"max_upload_mb":50}`))
	if e.TargetName != "instance settings" || e.TargetID != "" {
		t.Fatalf("wiped: %+v", e)
	}
	e = Entry{}
	takeCreatedTarget(&e, []byte(`{"id":"1","username":"bob"}`))
	if e.TargetID != "1" || e.TargetName != "bob" {
		t.Fatalf("created target: %+v", e)
	}
}

func TestRetentionIgnoresANonPositiveInterval(t *testing.T) {
	(&Log{DB: &fakeDB{}}).RunRetention(context.Background(), 7, 0)
}

func TestPruneDeletesInBatches(t *testing.T) {
	f := &batchDB{}
	n, err := (&Log{DB: f}).Prune(context.Background(), 7)
	if err != nil || n != 2*pruneBatch+10 || f.calls != 3 {
		t.Fatalf("n=%d err=%v calls=%d", n, err, f.calls)
	}
}

type batchDB struct {
	fakeDB
	calls int
}

func (b *batchDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	b.calls++
	if b.calls < 3 {
		return pgconn.NewCommandTag("DELETE " + strconv.Itoa(pruneBatch)), nil
	}
	return pgconn.NewCommandTag("DELETE 10"), nil
}

func TestRestrictToComparesTheColumnAsUUID(t *testing.T) {
	var f filter
	f.restrictTo(Viewer{Channels: []uuid.UUID{uuid.New()}})
	sql, _ := f.query(10)
	if strings.Contains(sql, "::text") || !strings.Contains(sql, "channel_id = ANY(") {
		t.Fatal(sql)
	}
}

func TestSnapshotLogsRealErrorsOnly(t *testing.T) {
	var buf strings.Builder
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(old)
	run := func(err error) {
		Snapshot(context.Background(), &fakeDB{row: errRowWith{err}}, ResSettings, "")
	}
	run(pgx.ErrNoRows)
	if buf.Len() != 0 {
		t.Fatalf("missing row logged: %s", buf.String())
	}
	run(errors.New("column gone"))
	if !strings.Contains(buf.String(), "audit snapshot failed") {
		t.Fatalf("real error not logged: %q", buf.String())
	}
}

type errRowWith struct{ err error }

func (r errRowWith) Scan(...any) error { return r.err }
