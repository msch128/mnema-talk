package gateway

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/bot"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/perm"
)

// Dispatch event names.
const (
	EventMessageCreate          = "MESSAGE_CREATE"
	EventMessageUpdate          = "MESSAGE_UPDATE"
	EventMessageDelete          = "MESSAGE_DELETE"
	EventMessageReactionsUpdate = "MESSAGE_REACTIONS_UPDATE"
	EventChannelsUpdate         = "CHANNELS_UPDATE"
	EventGuildMemberAdd         = "GUILD_MEMBER_ADD"
	EventInteractionCreate      = "INTERACTION_CREATE"
)

// event maps a hub event to its dispatch name and the intent that gates it.
func event(eventType string) (name string, intent int64, ok bool) {
	switch eventType {
	case events.MessageCreate:
		return EventMessageCreate, bot.IntentGuildMessages, true
	case events.MessageUpdate:
		return EventMessageUpdate, bot.IntentGuildMessages, true
	case events.MessageDelete:
		return EventMessageDelete, bot.IntentGuildMessages, true
	case events.MessageReaction:
		return EventMessageReactionsUpdate, bot.IntentGuildMessageReactions, true
	case events.ChannelsChanged:
		return EventChannelsUpdate, bot.IntentGuilds, true
	case events.MemberJoined:
		return EventGuildMemberAdd, bot.IntentGuildMembers, true
	}
	return "", 0, false
}

func broadcastToSessions(targets []*session, name string, msg json.RawMessage) {
	for _, s := range targets {
		s.enqueue(name, msg)
	}
}

func dispatchGlobalEvent(snap *perm.Snapshot, targets []*session, name string, payload any) {
	if name == EventChannelsUpdate {
		// The layout or someone's access changed: bots refetch /channels.
		broadcastToSessions(targets, name, json.RawMessage(`{}`))
		return
	}
	if name == EventGuildMemberAdd {
		enqueueMemberAdd(snap, targets, name, payload)
	}
}

// Observe is the hub's tap: it sees every event the hub publishes (channel is
// uuid.Nil for events to everybody) and queues it for the sessions whose
// intents ask for it and whose bot may view the channel right now. It never
// blocks.
func (g *Gateway) Observe(channel uuid.UUID, eventType string, payload any) {
	if g.Perms == nil {
		return
	}
	name, intent, ok := event(eventType)
	if !ok {
		return
	}
	targets := g.sessionsWith(intent)
	if len(targets) == 0 {
		return
	}
	snap, err := g.Perms.Snapshot(context.Background())
	if err != nil {
		return // fail closed
	}
	if channel != uuid.Nil {
		enqueueChannelEvent(snap, targets, channel, name, payload)
		return
	}
	dispatchGlobalEvent(snap, targets, name, payload)
}

// enqueueChannelEvent queues a channel's event for the bots that may view it.
func enqueueChannelEvent(snap *perm.Snapshot, targets []*session, channel uuid.UUID, name string, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	for _, s := range targets {
		if snap.Channel(perm.Subject{ID: s.userID}, channel).Has(perm.ViewChannel) {
			s.enqueue(name, data)
		}
	}
}

// enqueueMemberAdd queues a new member only for the bots that share a channel
// with them.
func enqueueMemberAdd(snap *perm.Snapshot, targets []*session, name string, payload any) {
	member, ok := memberOf(payload)
	if !ok {
		return
	}
	data, err := json.Marshal(member)
	if err != nil {
		return
	}
	theirs := snap.ChannelPermissions(perm.Subject{ID: member.ID})
	for _, s := range targets {
		for ch := range snap.ChannelPermissions(perm.Subject{ID: s.userID}) {
			if _, shared := theirs[ch]; shared {
				s.enqueue(name, data)
				break
			}
		}
	}
}

// BotMember is the member of GUILD_MEMBER_ADD: what the REST member routes
// show a bot.
type BotMember struct {
	ID          uuid.UUID `json:"id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name"`
	AvatarURL   string    `json:"avatar_url,omitempty"`
}

func memberOf(payload any) (BotMember, bool) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return BotMember{}, false
	}
	var m BotMember
	if json.Unmarshal(raw, &m) != nil || m.ID == uuid.Nil {
		return BotMember{}, false
	}
	return m, true
}

func (g *Gateway) sessionsWith(intent int64) []*session {
	g.mu.Lock()
	defer g.mu.Unlock()
	var out []*session
	for _, s := range g.sessions {
		if s.intents&intent != 0 {
			out = append(out, s)
		}
	}
	return out
}

// PermissionsChanged tells the sessions of the affected bots (all when nil)
// that what they can reach may have changed; events are filtered live, so
// nothing is delivered that they may no longer see, and this prompts a
// refetch of /channels. It implements perm.Notifier.
func (g *Gateway) PermissionsChanged(userIDs []uuid.UUID) {
	want := map[uuid.UUID]bool{}
	for _, id := range userIDs {
		want[id] = true
	}
	for _, s := range g.sessionsWith(bot.IntentGuilds) {
		if userIDs == nil || want[s.userID] {
			s.enqueue(EventChannelsUpdate, json.RawMessage(`{}`))
		}
	}
}

// DeliverInteraction sends INTERACTION_CREATE to one live session of the bot
// (the most recently started) and reports whether there was one. It needs no
// intent: only the bot's own commands produce interactions.
func (g *Gateway) DeliverInteraction(botID uuid.UUID, d json.RawMessage) bool {
	g.mu.Lock()
	var best *session
	for _, s := range g.byBot[botID] {
		if s.live() && (best == nil || s.startedAt.After(best.startedAt)) {
			best = s
		}
	}
	g.mu.Unlock()
	if best == nil {
		return false
	}
	best.enqueue(EventInteractionCreate, d)
	return true
}
