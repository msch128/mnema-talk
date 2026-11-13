package bot

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat/botapi"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/chat/messages"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// BotMessages edits and deletes a bot's own messages outside the bot's
// message routes: the responses to interactions and the messages components
// are on (the chat handler's bot API, botapi.API).
type BotMessages interface {
	EditBotMessage(ctx context.Context, bot *auth.User, ch, msgID uuid.UUID, e botapi.BotMessageEdit) (*core.Message, error)
	DeleteBotMessage(ctx context.Context, bot *auth.User, ch, msgID uuid.UUID) error
}

var errNoMessages = errors.New("bot commands: no message editor configured")

// parseEdit checks an update of a message or a note: each field left out
// (nil) stays as it is.
func parseEdit(content *string, embeds *[]messages.InEmbed, comps *[]core.ActionRow, am *botapi.BotAllowedMentions) (botapi.BotMessageEdit, error) {
	e := botapi.BotMessageEdit{AllowedMentions: (*messages.AllowedMentions)(am)}
	if content != nil {
		c, err := core.ValidateContent(*content, true)
		if err != nil {
			return e, err
		}
		e.Content = &c
	}
	if embeds != nil {
		em, err := messages.SanitizeEmbeds(*embeds)
		if err != nil {
			return e, httpx.ErrInvalidInput(err.Error())
		}
		e.Embeds = &em
	}
	if comps != nil {
		v, err := core.ValidateComponents(*comps)
		if err != nil {
			return e, err
		}
		e.Components = &v
	}
	return e, nil
}

// parseUpdate is parseEdit for the data of callback 7.
func parseUpdate(d *CallbackData) (botapi.BotMessageEdit, error) {
	if d == nil {
		return botapi.BotMessageEdit{}, httpx.ErrInvalidInput("data is required for type 7")
	}
	return parseEdit(d.Content, d.Embeds, d.Components, d.AllowedMentions)
}

// answerUpdate is callback 7: the message the component is on changes. When
// that message is gone meanwhile the answer is refused (10008) and the bot
// may still answer otherwise.
func (c *Commands) answerUpdate(r *http.Request, in *interaction, e botapi.BotMessageEdit) error {
	if err := c.store.takeAnswer(in, CallbackUpdate, false); err != nil {
		return err
	}
	if _, err := c.updateTarget(r, in, in.source, e); err != nil {
		c.store.undoAnswer(in)
		return err
	}
	return nil
}

// updateTarget applies e to a message of the bot (answered with the message)
// or to one of its notes (replaced in the member's view; nil message).
func (c *Commands) updateTarget(r *http.Request, in *interaction, target source, e botapi.BotMessageEdit) (*core.Message, error) {
	ctx := r.Context()
	bot := auth.UserFrom(ctx)
	if target.message != uuid.Nil {
		if c.Messages == nil {
			return nil, errNoMessages
		}
		return c.Messages.EditBotMessage(ctx, bot, in.channel, target.message, e)
	}
	n, ok := c.store.noteOf(target.note, in.botID)
	if !ok {
		return nil, core.ErrMessageNotFound
	}
	if _, _, err := botapi.CheckBotSend(ctx, c.Perms, c.DB, bot, in.channel); err != nil {
		return nil, err
	}
	if err := c.invokerSees(ctx, in.channel, n.invoker); err != nil {
		return nil, err
	}
	n = n.with(e)
	if n.content == "" && len(n.embeds) == 0 && len(n.components) == 0 {
		return nil, httpx.ErrInvalidInput("Cannot send an empty message").WithDiscordCode(httpx.DiscordCodeEmptyMessage)
	}
	c.store.keepNote(n)
	c.showNote(in.botName, in.avatar, n, false)
	return nil, nil
}

// with is the note after the edit e.
func (n note) with(e botapi.BotMessageEdit) note {
	if e.Content != nil {
		n.content = *e.Content
	}
	if e.Embeds != nil {
		n.embeds = *e.Embeds
	}
	if e.Components != nil {
		n.components = *e.Components
	}
	return n
}

// parseModal checks the modal of callback 9.
func (c *Commands) parseModal(d *CallbackData) (answerFunc, error) {
	if d == nil || d.Components == nil {
		return nil, httpx.ErrInvalidInput("data with custom_id, title and components is required for type 9")
	}
	form, err := core.ValidateModal(core.Modal{CustomID: d.CustomID, Title: d.Title, Components: *d.Components})
	if err != nil {
		return nil, err
	}
	return func(r *http.Request, in *interaction) error { return c.answerModal(r, in, form) }, nil
}

// answerModal is callback 9: the modal opens for the member, as the
// interaction_modal event to their sessions. The bot must still be able to
// view the channel.
func (c *Commands) answerModal(r *http.Request, in *interaction, form core.Modal) error {
	if err := c.store.takeAnswer(in, CallbackModal, false); err != nil {
		return err
	}
	_, p, err := botapi.ChannelPermsOf(r.Context(), c.Perms, auth.UserFrom(r.Context()), in.channel)
	if err == nil && !p.Has(perm.ViewChannel) {
		err = perm.ErrChannelHidden
	}
	if err == nil {
		err = c.invokerSees(r.Context(), in.channel, in.invoker)
	}
	if err != nil {
		c.store.undoAnswer(in)
		return err
	}
	m := &modal{id: uuid.New(), botID: in.botID, botUser: in.botUser, botName: in.botName, avatar: in.avatar,
		channel: in.channel, invoker: in.invoker, form: form, source: in.source}
	c.store.openModal(m)
	c.Events.SendToUsers([]uuid.UUID{in.invoker}, InteractionModal{
		BotAvatarURL: m.avatar, BotID: m.botID, BotName: m.botName, ChannelID: m.channel,
		Components: form.Components, ID: m.id, Title: form.Title,
	})
	return nil
}

// InteractionModal is a modal a bot opened for the member who caused the
// interaction (interaction_modal). Submitting it is POST
// /api/channels/{channelID}/modals/{id}. The fields are in the alphabetical
// order of their JSON keys, which must not change.
type InteractionModal struct {
	BotAvatarURL string    `json:"bot_avatar_url"`
	BotID        uuid.UUID `json:"bot_id" format:"uuid"`
	BotName      string    `json:"bot_name"`
	ChannelID    uuid.UUID `json:"channel_id" format:"uuid"`
	// Components are 1 to 5 action rows of one text input each.
	Components []core.ActionRow `json:"components"`
	// ID identifies the open modal.
	ID    uuid.UUID `json:"id" format:"uuid"`
	Title string    `json:"title"`
}

func (InteractionModal) EventName() string { return events.InteractionModal }

// deferAnswer takes callback 5 or 6 and tells the member that the bot is on
// it, so the web app can say it is taking longer instead of inviting another
// click.
func (c *Commands) deferAnswer(in *interaction, typ int, laterEphemeral bool) error {
	if err := c.store.takeAnswer(in, typ, laterEphemeral); err != nil {
		return err
	}
	c.Events.SendToUsers([]uuid.UUID{in.invoker}, InteractionDeferred{BotID: in.botID, ChannelID: in.channel, InteractionID: in.id})
	return nil
}

// InteractionDeferred tells the member who caused an interaction that its
// bot acknowledged it (callback 5 or 6) and the answer follows
// (interaction_deferred). interaction_id is the one the run or click
// answered with. The fields are in the alphabetical order of their JSON
// keys, which must not change.
type InteractionDeferred struct {
	BotID         uuid.UUID `json:"bot_id" format:"uuid"`
	ChannelID     uuid.UUID `json:"channel_id" format:"uuid"`
	InteractionID uuid.UUID `json:"interaction_id" format:"uuid"`
}

func (InteractionDeferred) EventName() string { return events.InteractionDeferred }
