package messages

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/automod"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/chat/mentions"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/perm"
)

// Actor is who posts: the account and the two facts about it that the gate
// cannot read from the permission service. Roles, overwrites, timeouts, the
// pending rules and the new member waits all come from the permission
// snapshot at the moment of the post.
type Actor struct {
	ID uuid.UUID
	// Owner marks the community's Owner, who holds every permission.
	Owner bool
	// Bot marks a post that comes in through the bot API. A bot is refused by
	// its permissions alone (the rules a member accepts and the new member
	// waits are for people) and its mentions follow allowed_mentions.
	Bot bool
}

// MemberActor is u posting as a member: in the app, by a built-in command or
// by a message they scheduled.
func MemberActor(u *auth.User) Actor {
	sub := u.Subject()
	return Actor{ID: sub.ID, Owner: sub.Owner}
}

// BotActor is the bot account u posting through the bot API.
func BotActor(u *auth.User) Actor {
	a := MemberActor(u)
	a.Bot = true
	return a
}

func (a Actor) subject() perm.Subject { return perm.Subject{ID: a.ID, Owner: a.Owner} }

// Service posts messages for every entry point that has an author with
// permissions: the app, the bot API, the built-in commands and scheduled
// messages. It knows nothing of HTTP; its errors are the ones the routes
// answer with.
type Service struct {
	DB    *db.Pool
	Perms *perm.Service
	// Events publishes message_create; nil publishes nothing.
	Events events.Publisher
	// Online resolves @here; nil means nobody is online.
	Online mentions.OnlineSource
	// Automod screens a message before it is stored; nil screens nothing.
	Automod *automod.Engine
}

// PostCommand is one message to post. What differs between the entry points
// is a field here, so that Post has one path and every difference is named.
type PostCommand struct {
	ChannelID uuid.UUID
	// Content is the text as it came in. Post trims and checks it.
	Content string
	// ParentID is the thread root of a thread reply, ReplyToID the quoted message.
	ParentID  *uuid.UUID
	ReplyToID *uuid.UUID
	// AttachmentIDs are staged files the message carries. They need Attach
	// files on top of Send messages, and the text may then be empty.
	AttachmentIDs []uuid.UUID
	// Embeds are a bot's embeds as they came in; a member's message has none.
	// A bot's message needs text or an embed.
	Embeds []InEmbed
	// AllowedMentions narrows whom a bot's message notifies. Without it a bot
	// notifies only the members it names, never @all, @here or a role: a bot
	// posts unattended and must ask for the wide mentions. A member's mentions
	// follow their permissions and ignore this field.
	AllowedMentions *AllowedMentions
	// LifetimeSeconds, when above 0, is the lifetime the sender chose: the
	// server deletes the message that long after it stored it.
	LifetimeSeconds int
	// Nonce is the client's id of this send. A second post with it returns
	// the first message instead of storing another (Posted.Replayed). Only the
	// app sends one: it retries on its own after a lost connection.
	Nonce *uuid.UUID
	// ContentChecked takes Content as it is. A scheduled message was checked
	// when it was stored and is sent as stored.
	ContentChecked bool
	// KeepReadMarker leaves the author's read marker where it is. Whoever
	// posts while looking at the channel has seen everything before their
	// message, so Post moves the marker up to it. The author of a scheduled
	// message is not reading the channel when the server sends it: what others
	// wrote since their last visit must stay unread.
	KeepReadMarker bool
	// InTx, when set, runs in the transaction that stores the message, before
	// the event for the outgoing webhooks is recorded. An error undoes the
	// message. A scheduled message deletes its own row here, so it is sent
	// once or not at all.
	InTx func(tx pgx.Tx, messageID uuid.UUID) error
}

// Posted is the outcome of Post.
type Posted struct {
	// Message is the stored message. It is nil in one case only: the nonce
	// collided with a concurrent post and its row was gone before it could be
	// read. Replayed is set then.
	Message *core.Message
	// Replayed says the nonce was used before: Message is the earlier one and
	// nothing was stored or published now.
	Replayed bool
}

// Post checks that actor may post cmd, stores the message, records it for the
// channel's outgoing webhooks in the same transaction, publishes
// message_create and moves the author's read marker.
//
// The order of the checks is fixed and the same for everyone: the permission
// gate, the channel, the attachments, the content, the thread and reply
// target; then automod and, inside the transaction, slow mode, a locked post
// and the direct message rules.
func (s *Service) Post(ctx context.Context, actor Actor, cmd PostCommand) (*Posted, error) {
	if cmd.Nonce != nil {
		// A retry is answered before the Send check: it only ever returns the
		// author's own message in this channel, and a member who was timed out
		// since the first attempt must not see a stored message turn into 403.
		// Seeing the channel is still required: someone who lost access gets
		// the normal refusal, not the message.
		if _, err := s.decide(ctx, actor, cmd.ChannelID, perm.ViewChannel); err != nil {
			return nil, err
		}
		if posted, err := s.replay(ctx, actor.ID, cmd); posted != nil || err != nil {
			return posted, err
		}
	}
	nm, rules, err := s.prepare(ctx, actor, cmd)
	if err != nil {
		return nil, err
	}
	// The message is read in the insert's transaction: an error means that
	// nothing was stored, never a stored message behind a failure.
	_, msg, err := insertMessage(ctx, s.inserter(), nm, rules, s.inTx(ctx, actor, cmd))
	if IsNonceConflict(err) {
		// A concurrent send of the same nonce won the race.
		posted, err := s.replay(ctx, actor.ID, cmd)
		if posted == nil && err == nil {
			posted = &Posted{Replayed: true}
		}
		return posted, err
	}
	if err != nil {
		return nil, err
	}
	s.announce(ctx, msg, cmd)
	return &Posted{Message: msg}, nil
}

func (s *Service) inserter() inserter {
	return inserter{db: s.DB, online: s.Online, automod: s.Automod, load: true}
}

// inTx is what runs next to the insert: the staged files become the
// message's attachments, then the caller's own step.
func (s *Service) inTx(ctx context.Context, actor Actor, cmd PostCommand) func(pgx.Tx, uuid.UUID) error {
	if len(cmd.AttachmentIDs) == 0 {
		return cmd.InTx
	}
	return func(tx pgx.Tx, id uuid.UUID) error {
		if err := claimAttachments(ctx, tx, id, actor.ID, cmd.ChannelID, cmd.AttachmentIDs); err != nil || cmd.InTx == nil {
			return err
		}
		return cmd.InTx(tx, id)
	}
}

// announce tells the channel about the message and moves the author's read
// marker up to it.
func (s *Service) announce(ctx context.Context, msg *core.Message, cmd PostCommand) {
	if s.Events == nil {
		return
	}
	publishCreated(s.Events, msg)
	if !cmd.KeepReadMarker {
		MarkReadAfterSend(ctx, s.DB, s.Events, msg)
	}
}
