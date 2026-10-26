package ws

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/emojitext"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/perm"
)

// A live reaction is a short-lived emoji a member sends to the Talk they are
// in. It reaches the Talk's members once and is stored nowhere (doc/talks.md).
const (
	// reactionsPer is how many reactions one member may send per
	// reactionWindow, over all their connections.
	reactionsPer   = 5
	reactionWindow = 10 * time.Second
)

// windowGate is a sliding-window rate limit per user.
type windowGate struct {
	mu sync.Mutex
	m  map[uuid.UUID][]time.Time
}

// allow records one use of id and reports whether fewer than n uses happened
// within the last per.
func (g *windowGate) allow(id uuid.UUID, now time.Time, n int, per time.Duration) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.m == nil {
		g.m = map[uuid.UUID][]time.Time{}
	}
	recent := g.m[id][:0]
	for _, t := range g.m[id] {
		if now.Sub(t) < per {
			recent = append(recent, t)
		}
	}
	if len(recent) >= n {
		g.m[id] = recent
		return false
	}
	g.m[id] = append(recent, now)
	return true
}

// forget drops what is held about a user who went offline.
func (g *windowGate) forget(id uuid.UUID) {
	g.mu.Lock()
	delete(g.m, id)
	g.mu.Unlock()
}

// handleTalkReaction relays a reaction to the Talk the member is in. An emoji
// that is invalid, unknown or over the allowance is dropped without a word.
func (c *Client) handleTalkReaction(ctx context.Context, payload json.RawMessage) {
	var in struct {
		Emoji string `json:"emoji"`
	}
	cur := c.currentVoice()
	if cur == nil || json.Unmarshal(payload, &in) != nil {
		return
	}
	h := c.hub
	if !h.reactionGate.allow(c.User.ID, time.Now(), reactionsPer, reactionWindow) {
		return
	}
	emoji, err := chat.ValidateEmoji(in.Emoji)
	if err != nil {
		return
	}
	snap, ok := h.permSnapshot(*cur)
	if !ok || !c.can(snap, *cur, perm.ViewChannel|perm.Connect|perm.AddReactions) || !h.knownEmoji(ctx, emoji) {
		return
	}
	h.sendToVoiceRoom(*cur, events.TalkReaction, map[string]any{"channel_id": *cur, "user_id": c.User.ID, "emoji": emoji})
}

// knownEmoji is true for a standard emoji and for a custom emoji that still
// exists. Every member may use every custom emoji of the community.
func (h *Hub) knownEmoji(ctx context.Context, emoji string) bool {
	id, custom := emojitext.ID(emoji)
	if !custom || h.DB == nil {
		return true
	}
	var exists bool
	err := h.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM custom_emoji WHERE id = $1)`, id).Scan(&exists)
	return err == nil && exists
}
