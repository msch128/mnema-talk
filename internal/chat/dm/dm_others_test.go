package dm

import (
	"testing"

	"github.com/mnema-xyz/talk/internal/chat/readstate"
)

func TestAnnouncedConversationCarriesNothingOfTheStartersSettings(t *testing.T) {
	dm := DirectMessage{NotifyLevel: readstate.NotifyMute, CanSend: true, UnreadCount: 3}.forOthers()
	if dm.NotifyLevel != readstate.NotifyInherit || dm.CanSend || dm.UnreadCount != 0 {
		t.Fatalf("event payload: %+v", dm)
	}
}
