package wire

import (
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/events"
)

// InteractionDeferred tells the member who caused an interaction that its
// bot acknowledged it (callback 5 or 6) and the answer follows
// (interaction_deferred). interaction_id is the one the run or click
// answered with. The fields are in the alphabetical order of their JSON
// keys, which must not change.
type InteractionDeferred struct {
	BotID         uuid.UUID `json:"bot_id" format:"uuid"`
	ChannelID     uuid.UUID `json:"channel_id" format:"uuid"`
	InteractionID uuid.UUID `json:"interaction_id" format:"uuid"`
}

func (InteractionDeferred) EventName() string { return events.InteractionDeferred }
