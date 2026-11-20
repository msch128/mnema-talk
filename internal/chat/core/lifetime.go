package core

import (
	"context"
	"fmt"
	"strconv"

	"github.com/mnema-xyz/talk/internal/db"
	"github.com/mnema-xyz/talk/internal/httpx"
)

// The bounds of a message's lifetime (doc/timed-messages.md). The database
// holds the same ones (migration 0106).
const (
	MinLifetimeSeconds = 60
	MaxLifetimeSeconds = 30 * 24 * 60 * 60
)

// ShortLifetimeSeconds is the lifetime below which a push carries no text.
const ShortLifetimeSeconds = 10 * 60

// MessageSettings are the community's settings for lifetimes and the
// auto-delete default (instance_settings).
type MessageSettings struct {
	// LifetimesEnabled lets members send messages with a lifetime.
	LifetimesEnabled bool `json:"message_lifetimes_enabled"`
	// LifetimeMaxSeconds is the longest lifetime anybody may choose.
	LifetimeMaxSeconds int `json:"message_lifetime_max_seconds" minimum:"60" maximum:"2592000"`
	// AutoDeleteDefaultHours is the period channels without a setting of
	// their own follow; 0 = off.
	AutoDeleteDefaultHours int `json:"auto_delete_default_hours" minimum:"0" maximum:"8760"`
	// AutoDeleteDefaultScope is what that default removes.
	AutoDeleteDefaultScope string `json:"auto_delete_default_scope" enums:"all,attachments"`
}

// LoadMessageSettings reads the settings.
func LoadMessageSettings(ctx context.Context, q db.Querier) (MessageSettings, error) {
	var s MessageSettings
	err := q.QueryRow(ctx, `
		SELECT message_lifetimes_enabled, message_lifetime_max_seconds, auto_delete_default_hours, auto_delete_default_scope
		FROM instance_settings`).Scan(&s.LifetimesEnabled, &s.LifetimeMaxSeconds, &s.AutoDeleteDefaultHours, &s.AutoDeleteDefaultScope)
	if err != nil {
		return s, fmt.Errorf("load message settings: %w", err)
	}
	return s, nil
}

// CheckLifetimeBounds refuses a lifetime outside 60 seconds to 30 days.
func CheckLifetimeBounds(seconds int) error {
	if seconds < MinLifetimeSeconds || seconds > MaxLifetimeSeconds {
		return httpx.ErrInvalidInput("lifetime_seconds must be 60 to 2592000")
	}
	return nil
}

// CheckLifetime refuses a lifetime the community does not allow now.
func (s MessageSettings) CheckLifetime(seconds int) error {
	if err := CheckLifetimeBounds(seconds); err != nil {
		return err
	}
	if !s.LifetimesEnabled {
		return httpx.ErrInvalidInput("messages with a lifetime are turned off in this community")
	}
	if seconds > s.LifetimeMaxSeconds {
		return httpx.ErrInvalidInput("lifetime_seconds is longer than this community allows (" + strconv.Itoa(s.LifetimeMaxSeconds) + " s)")
	}
	return nil
}

// LiveSQL holds for the messages row called alias while neither it nor its
// thread root has reached the end of its lifetime: a reply ends with its
// thread, also when it has no lifetime of its own. Every read of messages for
// anyone adds it: the sweeper removes ended rows (a root with its replies)
// within about a second, and this keeps a late sweep from showing one.
func LiveSQL(alias string) string {
	return "((" + alias + ".expires_at IS NULL OR " + alias + ".expires_at > now()) AND " + rootLiveSQL(alias) + ")"
}

// rootLiveSQL holds for the messages row called alias unless it is a reply
// whose thread root ended. A row without a parent compares with no root.
func rootLiveSQL(alias string) string {
	return "NOT EXISTS (SELECT 1 FROM messages ended_root WHERE ended_root.id = " + alias + ".parent_id AND ended_root.expires_at <= now())"
}

// LiveM is LiveSQL("m") as a constant, for query constants that call their
// messages row m (a test holds the two together).
const LiveM = "((m.expires_at IS NULL OR m.expires_at > now()) AND " +
	"NOT EXISTS (SELECT 1 FROM messages ended_root WHERE ended_root.id = m.parent_id AND ended_root.expires_at <= now()))"

// LiveMessages is the messages table without the rows whose lifetime ended,
// or whose thread root's did, for a FROM or JOIN clause:
// "FROM " + LiveMessages + " m".
// It spells out LiveSQL("live"), so that it stays a constant (a test holds the
// two together).
const LiveMessages = "(SELECT * FROM messages live WHERE ((live.expires_at IS NULL OR live.expires_at > now()) AND " +
	"NOT EXISTS (SELECT 1 FROM messages ended_root WHERE ended_root.id = live.parent_id AND ended_root.expires_at <= now())))"
