package audit

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// fakeDB is a Querier that records inserts and can fail on demand.
type fakeDB struct {
	mu       sync.Mutex
	execs    int
	execErr  error
	queryErr error
	rows     pgx.Rows
	row      pgx.Row
}

func (f *fakeDB) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.execs++
	return pgconn.NewCommandTag("DELETE 3"), f.execErr
}
func (f *fakeDB) Query(context.Context, string, ...any) (pgx.Rows, error) { return f.rows, f.queryErr }
func (f *fakeDB) QueryRow(context.Context, string, ...any) pgx.Row        { return f.row }

type errRow struct{}

func (errRow) Scan(...any) error { return errors.New("no row") }

// fakeRows yields n rows; scanErr fails the scan, err is reported at the end.
type fakeRows struct {
	n       int
	scanErr error
	err     error
}

func (r *fakeRows) Close()                                       {}
func (r *fakeRows) Err() error                                   { return r.err }
func (r *fakeRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *fakeRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *fakeRows) Next() bool                                   { r.n--; return r.n >= 0 }
func (r *fakeRows) Scan(...any) error                            { return r.scanErr }
func (r *fakeRows) Values() ([]any, error)                       { return nil, nil }
func (r *fakeRows) RawValues() [][]byte                          { return nil }
func (r *fakeRows) Conn() *pgx.Conn                              { return nil }
func (r *fakeRows) TypeMap() *pgtype.Map                         { return nil }

func TestRecordAndRetentionAgainstFakeDB(t *testing.T) {
	ctx := context.Background()
	f := &fakeDB{execErr: errors.New("down")}
	l := &Log{DB: f, Actor: func(context.Context) (uuid.UUID, string, bool) { return uuid.New(), "x", true }}
	if l.Record(ctx, Entry{Action: "a"}) == nil {
		t.Fatal("error swallowed")
	}
	l.RecordOrLog(ctx, Entry{Action: "a"}) // logs, does not panic
	f.execErr = nil
	if n, err := l.Prune(ctx, 7); n != 3 || err != nil {
		t.Fatalf("prune %d %v", n, err)
	}
	// RunRetention prunes at once, then on every tick, and stops with ctx.
	f2 := &fakeDB{execErr: errors.New("down")}
	rctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { (&Log{DB: f2}).RunRetention(rctx, 7, time.Millisecond); close(done) }()
	time.Sleep(5 * time.Millisecond)
	f2.mu.Lock()
	f2.execErr = nil
	f2.mu.Unlock()
	time.Sleep(5 * time.Millisecond)
	cancel()
	<-done
	f2.mu.Lock()
	defer f2.mu.Unlock()
	if f2.execs < 2 {
		t.Fatalf("pruned %d times", f2.execs)
	}
}

func TestSnapshotScanFailure(t *testing.T) {
	f := &fakeDB{row: errRow{}}
	if m, _ := Snapshot(context.Background(), f, ResRole, uuid.NewString()); m != nil {
		t.Fatal("snapshot of a missing row")
	}
}

func TestListFailures(t *testing.T) {
	for name, f := range map[string]*fakeDB{
		"query": {queryErr: errors.New("down")},
		"scan":  {rows: &fakeRows{n: 1, scanErr: errors.New("bad row")}},
		"rows":  {rows: &fakeRows{n: 0, err: errors.New("cut")}},
	} {
		l := &Log{DB: f}
		rec := httptest.NewRecorder()
		if err := l.list(rec, httptest.NewRequest("GET", "/api/admin/audit-log?target_id=x", nil)); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestMiddlewareRecordsUnknownRoutesGenerically(t *testing.T) {
	f := &fakeDB{}
	l := &Log{DB: f}
	h := l.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/api/admin/brand-new", nil))
	if f.execs != 1 {
		t.Fatalf("recorded %d", f.execs)
	}
}

func TestMatchRejectsEmptySegmentAndSanitizeCapsKeys(t *testing.T) {
	if _, _, ok := Match("POST", "/api/admin/users//disable"); ok {
		t.Fatal("empty id matched")
	}
	m := map[string]any{}
	for i := 0; i < 80; i++ {
		m[strings.Repeat("k", i+1)] = i
	}
	if got := sanitize(m, 0).(map[string]any); len(got) != maxItems {
		t.Fatalf("kept %d keys", len(got))
	}
}
