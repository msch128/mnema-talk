package push

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
)

// Direct kinds.
const (
	// DirectReminder is a reminder the member asked for (Remind me).
	DirectReminder = "reminder"
	// DirectScheduledFailed tells the author a scheduled message was refused.
	DirectScheduledFailed = "scheduled_failed"
)

// Direct is a notification addressed to one member by a feature other than a
// new message: it is not subject to channel mute levels, because the member
// asked for it. The caller has decided what the member may see: the ids are
// nil and Gone is set for a message they can no longer see.
type Direct struct {
	User uuid.UUID
	Kind string
	// ID makes the notification's tag unique per reminder.
	ID uuid.UUID
	// ChannelID and MessageID open the message when the notification is clicked.
	ChannelID *uuid.UUID
	MessageID *uuid.UUID
	ParentID  *uuid.UUID
	Sender    string
	Content   string
	// Gone is set when the member can no longer see the message.
	Gone bool
}

// buildDirect composes the push of a Direct for the member's language. The
// message text is only included when the member and the operator allow previews.
func buildDirect(d Direct, locale string, preview bool, ts int64) Payload {
	p := Payload{
		Version: 1, Type: typeMessage, Tag: d.Kind + ":" + d.ID.String(),
		ChannelID: d.ChannelID, MessageID: d.MessageID, ParentID: d.ParentID, Timestamp: ts,
	}
	switch {
	case d.Kind == DirectScheduledFailed:
		p.Title, p.Body = text(locale, "scheduled_failed"), text(locale, "scheduled_failed_body")
	case d.Gone:
		p.Title, p.Body = text(locale, "reminder"), text(locale, "reminder_gone")
	default:
		p.Title, p.Body = text(locale, "reminder"), text(locale, "reminder_body")
		if line := clean(plainLine(d.Content, locale), maxPreviewRunes); preview && line != "" {
			p.Body = clean(d.Sender, maxNameRunes) + ": " + line
		}
	}
	return p
}

// Direct sends d to the member's devices. It never blocks: a full queue drops
// the push (counted), as for messages. Failures are logged; the in-app
// notification does not depend on push.
func (s *Service) Direct(ctx context.Context, d Direct) {
	if s == nil {
		return
	}
	s.init()
	opts := Options{TTL: mentionTTL, Urgent: true, Topic: topic(d.ID, true), Timeout: sendTimeout}
	err := s.pushToMember(ctx, d.User, opts, func(locale string, preview bool) Payload {
		return buildDirect(d, locale, preview, s.Now().UnixMilli())
	})
	if err != nil {
		slog.Warn("push: direct", "err", err)
	}
}

// pushToMember queues the push compose builds for every device of the member:
// the one place that reads their language and preview choice (the operator's
// switch applies on top) and their subscriptions. A member with no device, or
// a payload that cannot be built, is an error only when the cause is the
// database.
func (s *Service) pushToMember(ctx context.Context, user uuid.UUID, opts Options, compose func(locale string, preview bool) Payload) error {
	locale, preview, err := loadPushPrefs(ctx, s.DB, user)
	if err != nil {
		return fmt.Errorf("load preferences: %w", err)
	}
	targets, err := targetsFor(ctx, s.DB, []uuid.UUID{user})
	if err != nil {
		return fmt.Errorf("load subscriptions: %w", err)
	}
	if len(targets[user]) == 0 {
		return nil
	}
	body, err := compose(locale, preview && s.Previews).encode()
	if err != nil {
		return fmt.Errorf("encode payload: %w", err)
	}
	s.queue(targets[user], body, opts)
	return nil
}
