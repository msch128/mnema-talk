// Package raid is raid protection at the door and for newcomers
// (doc/moderation.md): a throttle on sign-ups, a lockdown switch that stops
// them altogether, and the minimum membership age before a new member may
// write, react or speak. The age gates are enforced by package perm from the
// same settings, so REST, the WebSocket hub and the SFU agree.
//
// The package knows the settings and the door; it does not know HTTP
// sessions, so package auth can call it from the sign-up.
package raid

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// Bounds of the settings.
const (
	MaxJoinLimit      = 1000
	MinJoinWindow     = 1
	MaxJoinWindow     = 24 * 60
	MaxMinAgeMinutes  = 30 * 24 * 60
	MaxLockdownHours  = 30 * 24
	defaultJoinWindow = 10
)

// Settings are the raid protection settings as the admins see them.
type Settings struct {
	// JoinLimit is the most new members within JoinWindowMinutes; 0: no throttle.
	JoinLimit         int `json:"join_limit" minimum:"0" maximum:"1000"`
	JoinWindowMinutes int `json:"join_window_minutes" minimum:"1" maximum:"1440"`
	// MinAgeWriteMinutes, MinAgeReactMinutes and MinAgeSpeakMinutes are how
	// long after joining a new member may write, react and speak; 0: at once.
	MinAgeWriteMinutes int `json:"min_age_write_minutes" minimum:"0" maximum:"43200"`
	MinAgeReactMinutes int `json:"min_age_react_minutes" minimum:"0" maximum:"43200"`
	MinAgeSpeakMinutes int `json:"min_age_speak_minutes" minimum:"0" maximum:"43200"`
	// JoinedInWindow is how many members joined within the window right now.
	JoinedInWindow int      `json:"joined_in_window" minimum:"0"`
	Lockdown       Lockdown `json:"lockdown"`
}

// Lockdown is the state of the lockdown switch.
type Lockdown struct {
	// Active is true while sign-ups are stopped (switched on and not expired).
	Active bool `json:"active"`
	// PauseInvites also stops creating invites.
	PauseInvites bool `json:"pause_invites"`
	// Until is when the lockdown ends by itself; null: when switched off.
	Until *time.Time `json:"until,omitempty" binding:"optional" format:"date-time" extensions:"x-nullable"`
	// StartedAt is when it was switched on.
	StartedAt *time.Time `json:"started_at,omitempty" binding:"optional" format:"date-time" extensions:"x-nullable"`
}

// Input replaces the throttle and the age gates.
type Input struct {
	JoinLimit          int `json:"join_limit" minimum:"0" maximum:"1000"`
	JoinWindowMinutes  int `json:"join_window_minutes" minimum:"1" maximum:"1440"`
	MinAgeWriteMinutes int `json:"min_age_write_minutes" minimum:"0" maximum:"43200"`
	MinAgeReactMinutes int `json:"min_age_react_minutes" minimum:"0" maximum:"43200"`
	MinAgeSpeakMinutes int `json:"min_age_speak_minutes" minimum:"0" maximum:"43200"`
}

// LockdownInput switches the lockdown.
type LockdownInput struct {
	Active       bool `json:"active"`
	PauseInvites bool `json:"pause_invites" binding:"optional"`
	// Hours ends the lockdown by itself after this long (1 to 720); 0 or
	// omitted: it stays until switched off.
	Hours int `json:"hours" binding:"optional" minimum:"0" maximum:"720"`
}

// Validate checks the bounds.
func (in Input) Validate() error {
	switch {
	case in.JoinLimit < 0 || in.JoinLimit > MaxJoinLimit:
		return httpx.ErrInvalidInput(fmt.Sprintf("join_limit must be between 0 and %d", MaxJoinLimit))
	case in.JoinWindowMinutes < MinJoinWindow || in.JoinWindowMinutes > MaxJoinWindow:
		return httpx.ErrInvalidInput(fmt.Sprintf("join_window_minutes must be between %d and %d", MinJoinWindow, MaxJoinWindow))
	case badAge(in.MinAgeWriteMinutes):
		return httpx.ErrInvalidInput(fmt.Sprintf("min_age_write_minutes must be between 0 and %d", MaxMinAgeMinutes))
	case badAge(in.MinAgeReactMinutes):
		return httpx.ErrInvalidInput(fmt.Sprintf("min_age_react_minutes must be between 0 and %d", MaxMinAgeMinutes))
	case badAge(in.MinAgeSpeakMinutes):
		return httpx.ErrInvalidInput(fmt.Sprintf("min_age_speak_minutes must be between 0 and %d", MaxMinAgeMinutes))
	}
	return nil
}

func badAge(minutes int) bool { return minutes < 0 || minutes > MaxMinAgeMinutes }

// Validate checks the bounds.
func (in LockdownInput) Validate() error {
	if in.Hours < 0 || in.Hours > MaxLockdownHours {
		return httpx.ErrInvalidInput(fmt.Sprintf("hours must be between 0 and %d", MaxLockdownHours))
	}
	return nil
}

// ErrSignupsPaused is what a sign-up answers while the door is shut, by the
// lockdown or by the throttle. It is one calm state: wait and try later.
var ErrSignupsPaused = httpx.NewAPIError(http.StatusServiceUnavailable, httpx.CodeUnavailable, "sign-ups are paused for now: try again later")

// ErrInvitesPaused is what creating an invite answers during a lockdown that
// also pauses invites.
var ErrInvitesPaused = httpx.NewAPIError(http.StatusConflict, httpx.CodeConflict, "invites are paused during a lockdown")

const columns = `join_limit, join_window_minutes, min_age_write_minutes, min_age_react_minutes, min_age_speak_minutes,
	(SELECT COUNT(*)::int FROM users WHERE created_at > NOW() - make_interval(mins => join_window_minutes) AND NOT is_bot AND deleted_at IS NULL),
	COALESCE(lockdown AND (lockdown_until IS NULL OR lockdown_until > NOW()), FALSE), lockdown_pause_invites, lockdown_until, lockdown_started_at`

func scan(row pgx.Row) (Settings, error) {
	var s Settings
	err := row.Scan(&s.JoinLimit, &s.JoinWindowMinutes, &s.MinAgeWriteMinutes, &s.MinAgeReactMinutes, &s.MinAgeSpeakMinutes,
		&s.JoinedInWindow, &s.Lockdown.Active, &s.Lockdown.PauseInvites, &s.Lockdown.Until, &s.Lockdown.StartedAt)
	return s, err
}

// Get reads the settings.
func Get(ctx context.Context, q db.Querier) (Settings, error) {
	s, err := scan(q.QueryRow(ctx, `SELECT `+columns+` FROM instance_settings`))
	if err != nil {
		return Settings{}, fmt.Errorf("read raid protection: %w", err)
	}
	return s, nil
}

// Put replaces the throttle and the age gates.
func Put(ctx context.Context, q db.Querier, in Input) (Settings, error) {
	if err := in.Validate(); err != nil {
		return Settings{}, err
	}
	s, err := scan(q.QueryRow(ctx, `UPDATE instance_settings SET join_limit = $1, join_window_minutes = $2,
		min_age_write_minutes = $3, min_age_react_minutes = $4, min_age_speak_minutes = $5, updated_at = NOW()
		RETURNING `+columns, in.JoinLimit, in.JoinWindowMinutes, in.MinAgeWriteMinutes, in.MinAgeReactMinutes, in.MinAgeSpeakMinutes))
	if err != nil {
		return Settings{}, fmt.Errorf("write raid protection: %w", err)
	}
	return s, nil
}

// SetLockdown switches the lockdown on or off.
func SetLockdown(ctx context.Context, q db.Querier, in LockdownInput) (Settings, error) {
	if err := in.Validate(); err != nil {
		return Settings{}, err
	}
	var until *time.Time
	if in.Active && in.Hours > 0 {
		t := time.Now().Add(time.Duration(in.Hours) * time.Hour)
		until = &t
	}
	s, err := scan(q.QueryRow(ctx, `UPDATE instance_settings SET lockdown = $1,
		lockdown_pause_invites = $1 AND $2, lockdown_until = $3,
		lockdown_started_at = CASE WHEN $1 THEN COALESCE(lockdown_started_at, NOW()) ELSE NULL END, updated_at = NOW()
		RETURNING `+columns, in.Active, in.PauseInvites, until))
	if err != nil {
		return Settings{}, fmt.Errorf("write lockdown: %w", err)
	}
	return s, nil
}

// CheckSignup answers ErrSignupsPaused while a lockdown is on or more members
// joined within the throttle's window than it allows. Existing members never
// meet it.
func CheckSignup(ctx context.Context, q db.Querier) error {
	s, err := Get(ctx, q)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if s.Lockdown.Active || (s.JoinLimit > 0 && s.JoinedInWindow >= s.JoinLimit) {
		return ErrSignupsPaused
	}
	return nil
}

// CheckInvite answers ErrInvitesPaused while a lockdown that pauses invites is on.
func CheckInvite(ctx context.Context, q db.Querier) error {
	s, err := Get(ctx, q)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if s.Lockdown.Active && s.Lockdown.PauseInvites {
		return ErrInvitesPaused
	}
	return nil
}
