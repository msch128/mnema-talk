package wire

import (
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/events"
)

// UserDisabled is the short form of user_update: an account was disabled
// (banned, deactivated, deleted) or enabled again. The long form, a changed
// public profile, is auth.UserUpdate.
type UserDisabled struct {
	Disabled bool      `json:"disabled"`
	ID       uuid.UUID `json:"id" format:"uuid"`
}

func (UserDisabled) EventName() string { return events.UserUpdate }
