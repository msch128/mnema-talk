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
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/chat/messages"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/push"
)

// sendTimeout bounds the sending of one scheduled message.
const sendTimeout = 20 * time.Second

const (
	// maxSendAttempts is how many times a send may fail for a reason of our
	// own before the message becomes a failed draft.
	maxSendAttempts = 5
	// retryBase is the pause after the first failure; it doubles each time.
	retryBase = 30 * time.Second
)

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
	attempts  int
}

func (s *Service) loadDue(ctx context.Context) ([]dueScheduled, error) {
	rows, err := s.DB.Query(ctx, `
		SELECT id, user_id, channel_id, parent_id, reply_to_id, content, updated_at, attempts FROM scheduled_messages
		WHERE state = 'pending' AND send_at <= clock_timestamp() AND (retry_at IS NULL OR retry_at <= clock_timestamp()) ORDER BY send_at, id LIMIT $1`, batch)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (dueScheduled, error) {
		var d dueScheduled
		return d, row.Scan(&d.id, &d.user, &d.channel, &d.parent, &d.replyTo, &d.content, &d.updatedAt, &d.attempts)
	})
}

// sendScheduled sends the scheduled messages that fell due.
func (s *Service) sendScheduled(ctx context.Context) error {
	due, err := s.loadDue(ctx)
	if err != nil {
		if ctx.Err() == nil {
			slog.Warn("scheduled: load due", "err", err)
		}
		return err
	}
	for _, d := range due {
		if ctx.Err() != nil {
			return nil
		}
		sctx, cancel := context.WithTimeout(ctx, sendTimeout)
		s.sendOne(sctx, d)
		cancel()
	}
	return nil
}

// sendOne sends one message as its author through the normal send path: the
// permission model, slow mode, timeouts, the direct message rules and blocks all
// apply now, as for a message typed at this moment. A refusal turns the
// message into a failed draft and tells the author; a failure of our own (the
// database) leaves it waiting for a later round, with growing pauses, and turns
// it into a failed draft after maxSendAttempts tries.
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
		// ctx is the per-message context: once its own deadline ran out, any
		// failure (a closed connection, EOF) counts as an attempt of the send.
		// Only a cancelled context means the service is stopping; that stays
		// quiet and leaves the message untouched.
		if errors.Is(ctx.Err(), context.Canceled) {
			return
		}
		slog.Warn("scheduled: send", "err", err, "id", d.id, "attempt", d.attempts+1)
		s.retryLater(d)
	}
}

// retryLater books another try after a pause, or gives up on the message. It
// runs on its own context: the send may have used up its own.
func (s *Service) retryLater(d dueScheduled) {
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	if d.attempts+1 >= maxSendAttempts {
		s.fail(ctx, d, ReasonRefused)
		return
	}
	pause := retryBase << d.attempts
	_, err := s.DB.Exec(ctx, `
		UPDATE scheduled_messages SET attempts = attempts + 1, retry_at = clock_timestamp() + make_interval(secs => $3)
		WHERE id = $1 AND state = 'pending' AND updated_at = $2`, d.id, d.updatedAt, pause.Seconds())
	if err != nil {
		slog.Warn("scheduled: book retry", "err", err, "id", d.id)
	}
}

// post runs the send path. The scheduled row is removed in the transaction
// that stores the message, so a message is never sent twice nor lost half-way.
func (s *Service) post(ctx context.Context, d dueScheduled) (uuid.UUID, error) {
	user, ok, err := s.activeUser(ctx, d.user)
	if err != nil {
		return uuid.Nil, err
	}
	if !ok {
		return uuid.Nil, httpx.ErrForbidden("the account cannot send")
	}
	ctx, err = messages.SendContextFor(s.Perms, auth.WithUser(ctx, user), d.channel, perm.SendMessages)
	if err != nil {
		return uuid.Nil, err
	}
	if _, err := messages.ChannelForMessages(ctx, s.DB, d.channel); err != nil {
		return uuid.Nil, err
	}
	if err := messages.ValidateTarget(ctx, s.DB, d.channel, d.parent, d.replyTo); err != nil {
		return uuid.Nil, err
	}
	return messages.InsertMessage(ctx, s.DB, s.Online, messages.NewMessage{
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
	msg, err := messages.GetMessage(ctx, s.DB, messageID)
	if err != nil {
		slog.Warn("scheduled: load sent message", "err", err)
		return
	}
	// The author's read marker stays where it is: they are not reading the
	// channel now, and what others wrote since their last visit stays unread.
	if s.Events != nil {
		s.Events.BroadcastChannel(d.channel, (*core.MessageCreate)(msg))
	}
	s.notify(d.user, ScheduledEvent{ID: d.id, ChannelID: d.channel, State: "sent"})
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
	s.notify(d.user, ScheduledEvent{ID: d.id, ChannelID: d.channel, State: "failed", Reason: reason})
	if s.Push != nil {
		s.Push.Direct(ctx, push.Direct{User: d.user, Kind: push.DirectScheduledFailed, ID: d.id})
	}
}
