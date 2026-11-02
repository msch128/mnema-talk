package push

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// SendEventCommentNotice pushes a new comment on an event. Quiet presence and a
// muted Talk hold it back, as for the reminder. With message previews on, the
// body is "author: excerpt"; without, only that there is a new comment.
func (s *Service) SendEventCommentNotice(ctx context.Context, userID, eventID uuid.UUID, title, author, excerpt string, startTime time.Time, channelID *uuid.UUID) error {
	if s == nil || s.Sender == nil {
		return nil
	}
	s.init()
	if held, err := s.eventQuiet(ctx, userID, channelID); err != nil || held {
		return err
	}
	opts := Options{TTL: time.Hour, Urgent: false, Timeout: sendTimeout}
	err := s.pushToMember(ctx, userID, opts, func(locale string, preview bool) Payload {
		shown, body := s.Names.Get(ctx), text(locale, "event_comment")
		if preview {
			shown, body = title, fmt.Sprintf("%s: %s", author, excerpt)
		}
		return Payload{
			Version:   1,
			Type:      typeEvent,
			Title:     shown,
			Body:      body,
			Tag:       fmt.Sprintf("event-comment:%s", eventID),
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
