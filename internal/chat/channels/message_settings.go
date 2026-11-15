package channels

import (
	"context"
	"fmt"
	"time"

	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// LifetimeSettings is what every member reads of the lifetime settings: the
// composer offers lifetimes only while they are on, up to the ceiling.
type LifetimeSettings struct {
	// Enabled lets members send messages with a lifetime.
	Enabled bool `json:"message_lifetimes_enabled"`
	// MaxSeconds is the longest lifetime anybody may choose.
	MaxSeconds int `json:"message_lifetime_max_seconds" minimum:"60" maximum:"2592000"`
}

// MessageSettingsView is the answer of the administrators' message settings
// routes.
type MessageSettingsView struct {
	core.MessageSettings
	// AutoDeleteNextRunAt is when the auto-delete sweeper runs next.
	AutoDeleteNextRunAt time.Time `json:"auto_delete_next_run_at" format:"date-time"`
}

// LifetimeSettingsRequest is the body of PUT /api/admin/message-settings/lifetimes.
type LifetimeSettingsRequest struct {
	// Enabled switches lifetimes on or off; absent keeps it.
	Enabled *bool `json:"message_lifetimes_enabled" binding:"optional" extensions:"x-nullable"`
	// MaxSeconds is the new ceiling, 60 to 2592000; absent keeps it.
	MaxSeconds *int `json:"message_lifetime_max_seconds" minimum:"60" maximum:"2592000" binding:"optional" extensions:"x-nullable"`
}

// AutoDeleteDefaultRequest is the body of PUT /api/admin/message-settings/auto-delete
// and of its preview.
type AutoDeleteDefaultRequest struct {
	// Hours is the community default, 0 (off) to 8760.
	Hours int `json:"hours" minimum:"0" maximum:"8760"`
	// Scope is what it removes: "all" (the default) or "attachments".
	Scope string `json:"scope" enums:"all,attachments" binding:"optional"`
}

func (req AutoDeleteDefaultRequest) valid() (int, string, error) {
	scope, err := ValidAutoDeleteScope(req.Scope)
	if err != nil {
		return 0, "", err
	}
	if req.Hours < 0 || req.Hours > MaxAutoDeleteHours {
		return 0, "", httpx.ErrInvalidInput("hours must be 0 (off) to 8760")
	}
	return req.Hours, scope, nil
}

func messageSettingsView(ctx context.Context, q db.Querier) (MessageSettingsView, error) {
	s, err := core.LoadMessageSettings(ctx, q)
	return MessageSettingsView{MessageSettings: s, AutoDeleteNextRunAt: NextAutoDeleteRun(time.Now())}, err
}

// setLifetimeSettings stores the lifetime switch and ceiling.
func setLifetimeSettings(ctx context.Context, q db.Querier, req LifetimeSettingsRequest) error {
	if req.MaxSeconds != nil {
		if err := core.CheckLifetimeBounds(*req.MaxSeconds); err != nil {
			return httpx.ErrInvalidInput("message_lifetime_max_seconds must be 60 to 2592000")
		}
	}
	if _, err := q.Exec(ctx, `
		UPDATE instance_settings SET message_lifetimes_enabled = COALESCE($1, message_lifetimes_enabled),
			message_lifetime_max_seconds = COALESCE($2, message_lifetime_max_seconds), updated_at = NOW()`,
		req.Enabled, req.MaxSeconds); err != nil {
		return fmt.Errorf("set lifetime settings: %w", err)
	}
	return nil
}

// setAutoDeleteDefault stores the community's auto-delete default.
func setAutoDeleteDefault(ctx context.Context, q db.Querier, hours int, scope string) error {
	if _, err := q.Exec(ctx, `
		UPDATE instance_settings SET auto_delete_default_hours = $1, auto_delete_default_scope = $2, updated_at = NOW()`,
		hours, scope); err != nil {
		return fmt.Errorf("set auto-delete default: %w", err)
	}
	return nil
}
