//go:build integration

package messages

import (
	"context"
	"errors"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/chat/chattest"
	"github.com/msch128/mnema-talk/internal/chat/core"
)

func TestAuditSnapshotOfAMissingMessageIsEmpty(t *testing.T) {
	f := newFixture(t)
	h := &API{DB: f.p, Audit: &audit.Log{DB: f.p}}
	if h.snapshotMessage(context.Background(), uuid.New(), true) != nil {
		t.Fatal("snapshot of a message that does not exist")
	}
	if (&API{DB: f.p}).snapshotMessage(context.Background(), f.msg, true) != nil {
		t.Fatal("snapshot without an audit log")
	}
	// An edit or delete without a snapshot is simply not described further.
	h.auditEdit(context.Background(), f.msg, nil, "x")
	h.auditDelete(context.Background(), f.u, f.msg, nil, "")
}

func TestAuditSnapshotIsEmptyWhenTheChannelCannotBeRead(t *testing.T) {
	f := newFixture(t)
	broken := chattest.DriftPool(t, f.p, map[string]string{"channels": `SELECT id, name FROM public.channels`})
	h := &API{DB: broken, Audit: &audit.Log{DB: f.p}}
	if s := h.snapshotMessage(context.Background(), f.msg, false); s != nil {
		t.Fatalf("snapshot without a channel lookup: %+v", s)
	}
}

func TestAnchorLookupFailureIsNotAMissingMessage(t *testing.T) {
	f := newFixture(t)
	h := &API{DB: f.p}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	q := url.Values{"before": {f.msg.String()}}.Encode()
	r := chattest.Request(f.u, "", nil, q).WithContext(ctx)
	if _, err := h.parseChannelMessageQuery(r, f.ch); err == nil || errors.Is(err, core.ErrMessageNotFound) {
		t.Fatalf("channel anchor error = %v; want the lookup failure", err)
	}
	if _, err := h.parseThreadQuery(r, f.msg); err == nil || errors.Is(err, core.ErrMessageNotFound) {
		t.Fatalf("thread anchor error = %v; want the lookup failure", err)
	}
	live := chattest.Request(f.u, "", nil, url.Values{"before": {uuid.NewString()}}.Encode())
	if _, err := h.parseChannelMessageQuery(live, f.ch); !errors.Is(err, core.ErrMessageNotFound) {
		t.Fatalf("unknown anchor error = %v; want not found", err)
	}
}
