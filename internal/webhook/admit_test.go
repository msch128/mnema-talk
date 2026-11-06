package webhook

import (
	"testing"

	"github.com/google/uuid"
)

// A request the channel limit refuses must not use up the webhook's own
// budget: otherwise a busy channel starves a quiet webhook that never got
// its message through.
func TestAdmitChargesOnlyAdmittedRequests(t *testing.T) {
	h := &Handler{}
	h.SetLimits(2, 1)
	busy := &record{Webhook: Webhook{ID: uuid.New(), ChannelID: uuid.New()}}
	quiet := &record{Webhook: Webhook{ID: uuid.New(), ChannelID: busy.ChannelID}}

	if ok, _ := h.admit(busy); !ok {
		t.Fatal("first request refused")
	}
	for i := 0; i < 5; i++ {
		if ok, retry := h.admit(quiet); ok || retry <= 0 {
			t.Fatalf("channel limit not enforced: ok=%v retry=%v", ok, retry)
		}
	}
	// Other channels: the quiet webhook still has its whole budget.
	for i := 0; i < 2; i++ {
		quiet.ChannelID = uuid.New()
		if ok, _ := h.admit(quiet); !ok {
			t.Fatalf("request %d refused: channel-refused hits were charged to the webhook", i+1)
		}
	}
	quiet.ChannelID = uuid.New()
	if ok, _ := h.admit(quiet); ok {
		t.Fatal("webhook limit of 2 not enforced")
	}
}
