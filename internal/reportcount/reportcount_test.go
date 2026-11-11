package reportcount

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/events/wire"
	"github.com/msch128/mnema-talk/internal/perm"
)

type countRow struct {
	n   int
	err error
}

func (r countRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*(dest[0].(*int)) = r.n
	return nil
}

type fakeQuerier struct{ row countRow }

func (f fakeQuerier) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}
func (f fakeQuerier) QueryRow(context.Context, string, ...any) pgx.Row { return f.row }
func (f fakeQuerier) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unused")
}

func TestModeratorsGetTheCurrentOpenCount(t *testing.T) {
	rec := &events.Recorder{}
	Announce(context.Background(), fakeQuerier{countRow{n: 4, err: nil}}, rec)

	got := rec.Snapshot()
	if len(got) != 1 || got[0].Type != events.ReportsOpen || got[0].Need != perm.ManageReports {
		t.Fatalf("events = %+v, want one reports event for Manage reports holders", got)
	}
	if p, ok := got[0].Payload.(wire.ReportsOpen); !ok || p.Open != 4 {
		t.Fatalf("payload = %+v, want open 4", got[0].Payload)
	}
}

func TestNothingIsAnnouncedWhenTheCountFails(t *testing.T) {
	rec := &events.Recorder{}
	Announce(context.Background(), fakeQuerier{countRow{err: errors.New("db down")}}, rec)
	if got := rec.Snapshot(); len(got) != 0 {
		t.Fatalf("events = %+v, want none", got)
	}
	if _, err := Open(context.Background(), fakeQuerier{countRow{err: errors.New("db down")}}); err == nil {
		t.Fatal("Open returned no error for a failing query")
	}
}

func TestAnnouncingWithoutAPublisherDoesNothing(t *testing.T) {
	Announce(context.Background(), fakeQuerier{countRow{n: 1}}, nil)
}
