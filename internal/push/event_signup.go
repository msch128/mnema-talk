package push

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// SendEventSignupNotice pushes a change of a member's sign-up that they did
// not make themselves: kind "promoted" (they moved up from the waitlist of a
// role) or "removed" (a moderator removed them). Quiet presence and a muted
// Talk hold it back, as for the reminder.
func (s *Service) SendEventSignupNotice(ctx context.Context, userID, eventID uuid.UUID, title, kind, roleName string, startTime time.Time, channelID *uuid.UUID) error {
	if s == nil || s.Sender == nil {
		return nil
	}
	s.init()
	if held, err := s.eventQuiet(ctx, userID, channelID); err != nil || held {
		return err
	}
	key := "event_promoted"
	if kind == "removed" {
		key = "event_removed"
	}
	opts := Options{TTL: time.Hour, Urgent: false, Timeout: sendTimeout}
	err := s.pushToMember(ctx, userID, opts, func(locale string, preview bool) Payload {
		shown := s.Names.Get(ctx)
		if preview {
			shown = title
		}
		return Payload{
			Version:   1,
			Type:      typeEvent,
			Title:     shown,
			Body:      fmt.Sprintf(text(locale, key), roleName),
			Tag:       fmt.Sprintf("event-signup:%s", eventID),
			ChannelID: channelID,
			EventID:   &eventID,
			Timestamp: startTime.UnixMilli(),
		}
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	return err
}
