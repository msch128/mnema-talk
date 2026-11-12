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

// The snapshot is read in the transaction of the change it describes, so a
// failed read fails the change instead of leaving it without its entry
// (H-L7-04).
func TestAuditSnapshotOfAMissingMessageFails(t *testing.T) {
	f := newFixture(t)
	h := &API{DB: f.p, Audit: &audit.Log{DB: f.p}}
	if _, err := h.snapshotMessage(context.Background(), f.p, uuid.New(), true); !errors.Is(err, core.ErrMessageNotFound) {
		t.Fatalf("snapshot of a message that does not exist: %v", err)
	}
	if s, err := (&API{DB: f.p}).snapshotMessage(context.Background(), f.p, f.msg, true); s != nil || err != nil {
		t.Fatalf("snapshot without an audit log: %+v %v", s, err)
	}
}

func TestAuditSnapshotFailsWhenTheChannelCannotBeRead(t *testing.T) {
	f := newFixture(t)
	broken := chattest.DriftPool(t, f.p, map[string]string{"channels": `SELECT id, name FROM public.channels`})
	h := &API{DB: broken, Audit: &audit.Log{DB: f.p}}
	if s, err := h.snapshotMessage(context.Background(), broken, f.msg, false); s != nil || err == nil {
		t.Fatalf("snapshot without a channel lookup: %+v %v", s, err)
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
