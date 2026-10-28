package chat

import "testing"

func TestAnnouncedConversationCarriesNothingOfTheStartersSettings(t *testing.T) {
	dm := DirectMessage{NotifyLevel: NotifyMute, CanSend: true, UnreadCount: 3}.forOthers()
	if dm.NotifyLevel != NotifyInherit || dm.CanSend || dm.UnreadCount != 0 {
		t.Fatalf("event payload: %+v", dm)
	}
}
