package messages

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// grant is a passed permission check: what the actor may do in the channel
// and the snapshot it was decided on.
type grant struct {
	snap  *perm.Snapshot
	perms perm.Set
}

// decide checks need for actor in ch against the permissions as they are now.
func (s *Service) decide(ctx context.Context, actor Actor, ch uuid.UUID, need perm.Set) (grant, error) {
	return decide(ctx, s.Perms, actor, ch, need)
}

func decide(ctx context.Context, perms *perm.Service, actor Actor, ch uuid.UUID, need perm.Set) (grant, error) {
	snap, err := perms.ForChannel(ctx, ch)
	if err != nil {
		return grant{}, err
	}
	sub := actor.subject()
	g := grant{snap: snap, perms: snap.Channel(sub, ch)}
	if actor.Bot {
		// A bot never has rules to accept or a new member wait to sit out, so
		// its refusal is the plain one.
		return g, perm.CheckChannel(g.perms, need)
	}
	return g, snap.Deny(sub, g.perms, need)
}

// rules turns the grant into the rules of the insert.
func (g grant) rules(actor Actor, ch uuid.UUID, am *AllowedMentions) sendRules {
	rules := g.gateRules(actor)
	if actor.Bot {
		rules.Mentions = BotMentionPolicy(g.snap, ch, g.perms, am)
	} else {
		rules.Mentions = memberMentionPolicy(g.snap, ch, g.perms)
	}
	return rules
}

// gateRules are the rules without the mention policy.
func (g grant) gateRules(actor Actor) sendRules {
	// Whoever may manage messages in the channel, member or bot, is not held
	// back by slow mode or a locked post: they are the ones who moderate it.
	rules := sendRules{SlowModeExempt: g.perms.Has(perm.ManageMessages)}
	if !actor.Bot {
		rules.NewMemberWait = g.snap.NewMemberWaits(actor.ID).Write
	}
	return rules
}

// Refusal is the error Post answers before it looks at the content of a
// message without attachments, or nil: the permission gate and the channel.
// An entry point that must name this refusal ahead of a complaint about a
// body it could not read asks here.
func (s *Service) Refusal(ctx context.Context, actor Actor, ch uuid.UUID) error {
	if _, err := s.decide(ctx, actor, ch, perm.SendMessages); err != nil {
		return err
	}
	_, err := ChannelForMessages(ctx, s.DB, ch)
	return err
}

// prepare runs every check that needs no transaction and returns the message
// to insert with the rules of its author.
func (s *Service) prepare(ctx context.Context, actor Actor, cmd PostCommand) (NewMessage, sendRules, error) {
	need := perm.SendMessages
	if len(cmd.AttachmentIDs) > 0 {
		need |= perm.AttachFiles
	}
	g, err := s.decide(ctx, actor, cmd.ChannelID, need)
	if err != nil {
		return NewMessage{}, sendRules{}, err
	}
	if _, err := ChannelForMessages(ctx, s.DB, cmd.ChannelID); err != nil {
		return NewMessage{}, sendRules{}, err
	}
	if err := validateAttachmentIDs(cmd.AttachmentIDs); err != nil {
		return NewMessage{}, sendRules{}, err
	}
	content, embeds, err := cmd.clean(actor)
	if err != nil {
		return NewMessage{}, sendRules{}, err
	}
	if err := ValidateTarget(ctx, s.DB, s.Perms, actor.subject(), cmd.ChannelID, cmd.ParentID, cmd.ReplyToID); err != nil {
		return NewMessage{}, sendRules{}, err
	}
	nm := NewMessage{
		ChannelID: cmd.ChannelID, UserID: actor.ID, Content: content, ParentID: cmd.ParentID, ReplyToID: cmd.ReplyToID,
		Embeds: embeds, HasAttachment: len(cmd.AttachmentIDs) > 0, Nonce: cmd.Nonce,
	}
	return nm, g.rules(actor, cmd.ChannelID, cmd.AllowedMentions), nil
}

// clean checks the content of the command.
func (cmd PostCommand) clean(actor Actor) (string, []core.Embed, error) {
	switch {
	case cmd.ContentChecked:
		return cmd.Content, nil, nil
	case actor.Bot:
		return ValidateBotMessage(cmd.Content, cmd.Embeds, cmd.HasComponents)
	}
	content, err := core.ValidateContent(cmd.Content, len(cmd.AttachmentIDs) > 0)
	return content, nil, err
}

// ValidateBotMessage checks the content and embeds a bot sends (a message or
// a response to a command): the same rules, one message needs one of them or
// components (hasComponents: the caller validated them).
func ValidateBotMessage(rawContent string, rawEmbeds []InEmbed, hasComponents bool) (string, []core.Embed, error) {
	content, err := core.ValidateContent(rawContent, true)
	if err != nil {
		return "", nil, err
	}
	embeds, err := SanitizeEmbeds(rawEmbeds)
	if err != nil {
		return "", nil, httpx.ErrInvalidInput(err.Error())
	}
	if content == "" && len(embeds) == 0 && !hasComponents {
		return "", nil, httpx.ErrInvalidInput("Cannot send an empty message").WithDiscordCode(httpx.DiscordCodeEmptyMessage)
	}
	return content, embeds, nil
}

// replay answers a post whose nonce the author already used with the message
// it created, or with 410 when that message is gone. It returns nil when the
// nonce is new. A nonce of the same author in another channel is refused.
func (s *Service) replay(ctx context.Context, author uuid.UUID, cmd PostCommand) (*Posted, error) {
	var id *uuid.UUID
	var inChannel uuid.UUID
	err := s.DB.QueryRow(ctx, `SELECT message_id, channel_id FROM message_nonces WHERE user_id = $1 AND nonce = $2`, author, *cmd.Nonce).Scan(&id, &inChannel)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if inChannel != cmd.ChannelID {
		return nil, httpx.ErrInvalidInput("nonce was already used in another conversation")
	}
	if id == nil {
		return nil, errNonceMessageDeleted
	}
	msg, err := GetMessage(ctx, s.DB, *id)
	if err != nil {
		return nil, err
	}
	msg.Nonce = cmd.Nonce
	return &Posted{Message: msg, Replayed: true}, nil
}
