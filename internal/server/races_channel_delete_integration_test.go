//go:build integration

package server

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/testutil/racetest"
)

// Deleting a channel (the route, and the undo of a Discord import, through
// DeleteChannelsTx) removes its messages by
// cascade and then the hidden bot account of its incoming webhook. A delete of
// one of that webhook's messages takes the author's row before the message's
// (lockMessageForDelete), so the channel delete must take the bot account's
// row before the cascade too: the other order is a lock cycle (review round 2
// of the timed messages, finding 1). The channel's many messages keep the
// cascade busy long enough for the two to overlap.

const webhookMessages = 20000

type channelDeleteWorld struct {
	a     *app
	owner *client
}

// webhookChannel makes a channel with an incoming webhook and many of its
// messages, and returns the channel and its oldest webhook message.
func (w *channelDeleteWorld) webhookChannel(t *testing.T, round int) (ch, msg uuid.UUID) {
	t.Helper()
	res := w.owner.post("/api/admin/channels", map[string]any{"name": fmt.Sprintf("hooked-%d", round), "type": "text"})
	wantStatus(t, "channel", res, http.StatusCreated)
	res.decodeID(t, &ch)
	wantStatus(t, "webhook", w.owner.post("/api/admin/channels/"+ch.String()+"/webhooks", map[string]any{"name": "feed"}), http.StatusCreated)
	ctx := context.Background()
	if err := w.a.db.QueryRow(ctx, `
		WITH bot AS (SELECT id, bot_user_id FROM webhooks WHERE channel_id = $1)
		INSERT INTO messages (channel_id, user_id, webhook_id, content, created_at)
		SELECT $1, bot.bot_user_id, bot.id, 'feed ' || n, now() - make_interval(secs => $2 - n)
		FROM bot, generate_series(1, $2) n
		RETURNING id`, ch, webhookMessages).Scan(&msg); err != nil {
		t.Fatal(err)
	}
	return ch, msg
}

func TestDeletingAWebhooksChannelWhileItsMessageIsDeletedDoesNotDeadlock(t *testing.T) {
	a := newTimedApp(t)
	w := &channelDeleteWorld{a: a, owner: a.seedAdmin()}
	var ch, msg uuid.UUID
	racetest.Sweep(t, racetest.Rounds(3, 20), 2,
		func(round int) { ch, msg = w.webhookChannel(t, round) },
		func(round, i int) error {
			if i == 1 {
				// Once the cascade is under way: it holds the oldest message already.
				time.Sleep(time.Duration(5+round%4*5) * time.Millisecond)
				return statusErr(w.owner.delete("/api/channels/"+ch.String()+"/messages/"+msg.String()), http.StatusNoContent, http.StatusNotFound)
			}
			return statusErr(w.owner.delete("/api/admin/channels/"+ch.String()), http.StatusNoContent, http.StatusOK)
		},
		func(round int, r racetest.Round) string {
			for i, err := range r.Errs {
				if err != nil {
					return fmt.Sprintf("round %d caller %d: %v", round, i, err)
				}
			}
			if n := a.count(`SELECT COUNT(*) FROM channels WHERE id = $1`, ch); n != 0 {
				return fmt.Sprintf("round %d: the channel is still there", round)
			}
			return ""
		}).FailOnViolation(t)
}
