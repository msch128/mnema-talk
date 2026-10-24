package later

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/push"
)

// sendTimeout bounds the sending of one scheduled message.
const sendTimeout = 20 * time.Second

// errSuperseded stops a send whose scheduled message was edited, moved or
// deleted since it was read: the new state decides.
var errSuperseded = errors.New("scheduled message changed")

type dueScheduled struct {
	id        uuid.UUID
	user      uuid.UUID
	channel   uuid.UUID
	parent    *uuid.UUID
	replyTo   *uuid.UUID
	content   string
	updatedAt time.Time
}

func (s *Service) loadDue(ctx context.Context) ([]dueScheduled, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT id, user_id, channel_id, parent_id, reply_to_id, content, updated_at FROM scheduled_messages
		WHERE state = 'pending' AND send_at <= clock_timestamp() ORDER BY send_at, id LIMIT $1`, batch)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (dueScheduled, error) {
		var d dueScheduled
		return d, row.Scan(&d.id, &d.user, &d.channel, &d.parent, &d.replyTo, &d.content, &d.updatedAt)
	})
}

// sendScheduled sends the scheduled messages that fell due.
func (s *Service) sendScheduled(ctx context.Context) {
	due, err := s.loadDue(ctx)
	if err != nil {
		if ctx.Err() == nil {
			slog.Warn("scheduled: load due", "err", err)
		}
		return
	}
	for _, d := range due {
		if ctx.Err() != nil {
			return
		}
		sctx, cancel := context.WithTimeout(ctx, sendTimeout)
		s.sendOne(sctx, d)
		cancel()
	}
}

// sendOne sends one message as its author through the normal send path: the
// permission model, slow mode, timeouts, the direct message rules and blocks all
// apply now, as for a message typed at this moment. A refusal turns the
// message into a failed draft and tells the author; a failure of our own (the
// database) leaves it waiting for the next round.
func (s *Service) sendOne(ctx context.Context, d dueScheduled) {
	id, err := s.post(ctx, d)
	switch {
	case err == nil:
		s.announceSent(ctx, d, id)
	case errors.Is(err, errSuperseded):
	default:
		var apiErr *httpx.APIError
		if errors.As(err, &apiErr) {
			s.fail(ctx, d, reasonFor(apiErr))
			return
		}
		if ctx.Err() == nil {
			slog.Warn("scheduled: send", "err", err)
		}
	}
}

// post runs the send path. The scheduled row is removed in the transaction
// that stores the message, so a message is never sent twice nor lost half-way.
func (s *Service) post(ctx context.Context, d dueScheduled) (uuid.UUID, error) {
	sub, ok, err := s.subjectOf(ctx, d.user)
	if err != nil {
		return uuid.Nil, err
	}
	if !ok {
		return uuid.Nil, httpx.ErrForbidden("the account cannot send")
	}
	user, err := auth.GetUser(ctx, s.DB, sub.ID)
	if err != nil {
		return uuid.Nil, err
	}
	ctx, err = chat.SendContextFor(s.Perms, auth.WithUser(ctx, user), d.channel, perm.SendMessages)
	if err != nil {
		return uuid.Nil, err
	}
	if _, err := chat.ChannelForMessages(ctx, s.DB, d.channel); err != nil {
		return uuid.Nil, err
	}
	if err := chat.ValidateTarget(ctx, s.DB, d.channel, d.parent, d.replyTo); err != nil {
		return uuid.Nil, err
	}
	return chat.InsertMessage(ctx, s.DB, s.Online, chat.NewMessage{
		ChannelID: d.channel, UserID: d.user, Content: d.content, ParentID: d.parent, ReplyToID: d.replyTo,
	}, func(tx pgx.Tx, _ uuid.UUID) error {
		tag, err := tx.Exec(ctx, `DELETE FROM scheduled_messages WHERE id = $1 AND state = 'pending' AND updated_at = $2`, d.id, d.updatedAt)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return errSuperseded
		}
		return nil
	})
}

// reasonFor words a refusal as a code. A 403 covers permissions, timeouts,
// blocks and the direct message rules alike, and stays unspecific on purpose.
func reasonFor(e *httpx.APIError) string {
	switch e.Status {
	case http.StatusTooManyRequests:
		return ReasonSlowMode
	case http.StatusForbidden:
		return ReasonNotAllowed
	case http.StatusNotFound:
		return ReasonUnavailable
	}
	return ReasonRefused
}

func (s *Service) announceSent(ctx context.Context, d dueScheduled, messageID uuid.UUID) {
	msg, err := chat.GetMessage(ctx, s.DB, messageID)
	if err != nil {
		slog.Warn("scheduled: load sent message", "err", err)
		return
	}
	if s.Events != nil {
		s.Events.BroadcastChannel(d.channel, events.MessageCreate, msg)
	}
	s.notify(d.user, events.ScheduledUpdate, ScheduledEvent{ID: d.id, ChannelID: d.channel, State: "sent"})
}

// fail keeps a refused message as a draft and tells its author, in the app and
// by push. It only acts on the version that was read.
func (s *Service) fail(ctx context.Context, d dueScheduled, reason string) {
	tag, err := s.DB.Exec(ctx, `
		UPDATE scheduled_messages SET state = 'failed', failure_reason = $3, updated_at = clock_timestamp()
		WHERE id = $1 AND state = 'pending' AND updated_at = $2`, d.id, d.updatedAt, reason)
	if err != nil {
		if ctx.Err() == nil {
			slog.Warn("scheduled: mark failed", "err", err)
		}
		return
	}
	if tag.RowsAffected() != 1 {
		return
	}
	s.notify(d.user, events.ScheduledUpdate, ScheduledEvent{ID: d.id, ChannelID: d.channel, State: "failed", Reason: reason})
	if s.Push != nil {
		s.Push.Direct(ctx, push.Direct{User: d.user, Kind: push.DirectScheduledFailed, ID: d.id})
	}
}
