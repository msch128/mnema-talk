package communityevents

import (
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/events"
)

func TestBroadcastNeverCarriesTheCallersOwnAnswer(t *testing.T) {
	rec := &events.Recorder{}
	h := &Handler{Events: rec}
	mine := "going"
	ev := &CommunityEvent{ID: uuid.New(), MyRSVP: &mine}
	h.broadcast(ev)

	sent := rec.Snapshot()
	if len(sent) != 1 {
		t.Fatalf("broadcasts: %d", len(sent))
	}
	if got := sent[0].Payload.(*CommunityEvent); got.MyRSVP != nil || got.ID != ev.ID {
		t.Fatalf("payload: %+v", got)
	}
	if ev.MyRSVP == nil {
		t.Fatal("the caller's own copy lost its answer")
	}
}
