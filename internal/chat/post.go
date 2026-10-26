package chat

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/automod"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// Posting a message is shared by plain messages and uploads (media package).

// ChannelForMessages loads the channel a new message is posted to. Every
// channel has a text chat, voice channels included (the chat next to the
// Talk), so this only checks that it exists.
func ChannelForMessages(ctx context.Context, p *db.Pool, chID uuid.UUID) (*ChannelInfo, error) {
	return LoadChannel(ctx, p, chID)
}

// ValidateTarget checks a new message's thread parent and quoted reply target.
func ValidateTarget(ctx context.Context, p *db.Pool, chID uuid.UUID, parentID, replyToID *uuid.UUID) error {
	if err := ValidateParent(ctx, p, chID, parentID); err != nil {
		return err
	}
	return ValidateReplyTarget(ctx, p, chID, parentID, replyToID)
}

// InsertMessage stores a validated message and its mentions in one
// transaction. also, when set, runs in the same transaction (e.g. to record
// an attachment).
func InsertMessage(ctx context.Context, p *db.Pool, online OnlineSource, nm NewMessage, also func(tx pgx.Tx, messageID uuid.UUID) error) (uuid.UUID, error) {
	pending, err := checkAutomod(ctx, p, nm)
	if err != nil {
		return uuid.Nil, err
	}
	var id uuid.UUID
	err = pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		var err error
		if id, err = insertMessageTx(ctx, tx, online, nm); err != nil {
			return err
		}
		if also != nil {
			return also(tx, id)
		}
		return nil
	})
	if err != nil {
		return uuid.Nil, err
	}
	pending.Commit(ctx, id)
	return id, nil
}

// checkAutomod applies the automod rules to a member's new message before it
// is stored: a rule that blocks refuses it, one that only flags is recorded
// once the message exists (Pending.Commit). Webhook messages are not checked.
func checkAutomod(ctx context.Context, p *db.Pool, nm NewMessage) (*automod.Pending, error) {
	if nm.WebhookID != nil {
		return nil, nil
	}
	parts := append([]string{}, nm.ScreenText...)
	if nm.Title != nil {
		parts = append(parts, *nm.Title)
	}
	content := strings.Join(append(parts, nm.Content), "\n")
	return automod.Of(p).Check(ctx, automod.Post{ChannelID: nm.ChannelID, UserID: nm.UserID, Content: content, HasAttachment: nm.HasAttachment})
}

// screenText applies the automod rules to member-written text that is not a
// message body (a forum title edit, a tag name). messageID is the post being
// edited, or uuid.Nil.
func screenText(ctx context.Context, p *db.Pool, channelID, userID, messageID uuid.UUID, text string) (*automod.Pending, error) {
	return automod.Of(p).Check(ctx, automod.Post{ChannelID: channelID, UserID: userID, Content: text, MessageID: messageID})
}

// insertMessageTx stores the message, its mentions and, for a thread reply,
// the follows it implies.
func insertMessageTx(ctx context.Context, tx pgx.Tx, online OnlineSource, nm NewMessage) (uuid.UUID, error) {
	if err := gatePost(ctx, tx, nm); err != nil {
		return uuid.Nil, err
	}
	id, err := CreateMessage(ctx, tx, nm)
	if err != nil {
		return uuid.Nil, err
	}
	if err := RecordMentions(ctx, tx, online, id, nm.UserID, nm.Content); err != nil {
		return uuid.Nil, err
	}
	if nm.ParentID != nil {
		if err := followOnReply(ctx, tx, id, *nm.ParentID, nm.UserID); err != nil {
			return uuid.Nil, err
		}
	}
	return id, nil
}

type slowModeExemptKey struct{}

// WithSlowModeExempt marks a request whose author is not held back by slow
// mode (they may manage messages in the channel).
func WithSlowModeExempt(ctx context.Context, exempt bool) context.Context {
	return context.WithValue(ctx, slowModeExemptKey{}, exempt)
}

func slowModeExempt(ctx context.Context) bool {
	v, _ := ctx.Value(slowModeExemptKey{}).(bool)
	return v
}

// enforceSlowMode refuses a message that comes too soon after the author's
// last one in the channel and otherwise records this post as the last one.
// The time of the last post lives in its own table, so deleting a message does
// not reset the wait, and the upsert runs in the insert's transaction: its row
// lock makes a concurrent post wait and then see the new time, so two requests
// cannot both slip through the same gap.
func enforceSlowMode(ctx context.Context, tx pgx.Tx, channelID, userID uuid.UUID, seconds int) error {
	if seconds == 0 {
		return nil
	}
	tag, err := tx.Exec(ctx, `
		INSERT INTO slow_mode_posts (channel_id, user_id, last_post_at) VALUES ($1, $2, clock_timestamp())
		ON CONFLICT (channel_id, user_id) DO UPDATE SET last_post_at = clock_timestamp()
		WHERE slow_mode_posts.last_post_at + make_interval(secs => $3) <= clock_timestamp()`,
		channelID, userID, float64(seconds))
	if err != nil {
		return fmt.Errorf("record post for slow mode: %w", err)
	}
	if tag.RowsAffected() == 1 {
		return nil
	}
	var wait float64
	if err := tx.QueryRow(ctx, `
		SELECT EXTRACT(EPOCH FROM (last_post_at + make_interval(secs => $3) - clock_timestamp()))
		FROM slow_mode_posts WHERE channel_id = $1 AND user_id = $2`, channelID, userID, float64(seconds)).Scan(&wait); err != nil {
		return fmt.Errorf("check slow mode: %w", err)
	}
	wait = math.Max(wait, 0.001)
	return &httpx.APIError{
		Status: http.StatusTooManyRequests, Code: httpx.CodeRateLimited, RetryAfter: time.Duration(wait * float64(time.Second)),
		Message: fmt.Sprintf("slow mode: wait %d s before posting again", int(math.Ceil(wait))),
	}
}

// PruneSlowMode forgets post times older than the longest slow mode: they can
// no longer hold anybody back.
func PruneSlowMode(ctx context.Context, q db.Querier) (int64, error) {
	tag, err := q.Exec(ctx, `DELETE FROM slow_mode_posts WHERE last_post_at < clock_timestamp() - make_interval(secs => $1)`, float64(MaxSlowModeSeconds))
	return tag.RowsAffected(), err
}

// RunSlowModePrune prunes every interval until ctx ends.
func RunSlowModePrune(ctx context.Context, q db.Querier, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if _, err := PruneSlowMode(ctx, q); err != nil && ctx.Err() == nil {
				slog.Warn("slow mode prune failed", "err", err)
			}
		}
	}
}

// PublishMessage loads a stored message, broadcasts message_create and
// writes it as the 201 response.
func PublishMessage(w http.ResponseWriter, r *http.Request, p *db.Pool, pub events.Publisher, id uuid.UUID) error {
	msg, err := GetMessage(r.Context(), p, id)
	if err != nil {
		return err
	}
	pub.BroadcastChannel(msg.ChannelID, events.MessageCreate, msg)
	httpx.WriteJSON(w, http.StatusCreated, msg)
	return nil
}

// queryIDs parses the named optional UUID query parameters into their
// destinations and returns how many were given.
func queryIDs(r *http.Request, what string, dst map[string]**uuid.UUID) (int, error) {
	n := 0
	for name, d := range dst {
		raw := r.URL.Query().Get(name)
		if raw == "" {
			continue
		}
		id, err := uuid.Parse(raw)
		if err != nil {
			return 0, httpx.ErrInvalidInput(name + " must be " + what)
		}
		*d = &id
		n++
	}
	return n, nil
}

// firstID returns the first non-nil id.
func firstID(ids ...*uuid.UUID) *uuid.UUID {
	for _, id := range ids {
		if id != nil {
			return id
		}
	}
	return nil
}
