package botapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/chat/mentions"
	"github.com/msch128/mnema-talk/internal/chat/messages"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// BotEditMessageRequest is the body of editing a bot's message. Both fields
// are optional; one that is left out stays as it is.
type BotEditMessageRequest struct {
	// Content is the new text. It may be empty while the message keeps an
	// attachment, embeds or components.
	Content *string `json:"content" maxLength:"4000" binding:"optional" extensions:"x-nullable"`
	// Components replace the message's buttons and select menus; [] removes them.
	Components []core.ActionRow `json:"components" binding:"optional"`
}

// rawBotEdit is BotEditMessageRequest as it is read: the components are
// decoded on their own, so a field of Discord's that Mnema Talk does not use
// is ignored there like in a new message.
type rawBotEdit struct {
	Content    *string         `json:"content"`
	Components json.RawMessage `json:"components"`
}

// BotMessageEdit is an edit of a bot's own message. A nil field keeps what
// the message has; every other field is already checked.
type BotMessageEdit struct {
	Content    *string
	Embeds     *[]core.Embed
	Components *[]core.ActionRow
	// AllowedMentions narrows whom the new text notifies, as on a post; nil is
	// the policy of a post without it (members named, never @all, @here or a role).
	AllowedMentions *messages.AllowedMentions
}

// DecodeComponents reads the components of a request body that were kept raw
// and validates them. Absent (or null) is nil: the message keeps its own.
func DecodeComponents(raw json.RawMessage) (*[]core.ActionRow, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var in []core.ActionRow
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, httpx.ErrInvalidInput("components must be a list of action rows")
	}
	comps, err := core.ValidateComponents(in)
	if err != nil {
		return nil, err
	}
	return &comps, nil
}

// botEditMessage handles PATCH /api/bot/v1/channels/{channelID}/messages/{messageID}.
//
// @Summary Edit the bot's own message
// @Description Only the bot's own messages; needs Send messages in the channel. content and components are each optional and stay as they are when left out; "components": [] removes them. The content may be empty while the message keeps an attachment, embeds or components. An edit that keeps the content does not mark the message edited. Broadcasts message_update (MESSAGE_UPDATE on the gateway).
// @ID botEditMessage
// @Tags Bot API
// @Accept json
// @Produce json
// @Security botAuth
// @Param channelID path string true "Channel ID." Format(uuid)
// @Param messageID path string true "Message ID." Format(uuid)
// @Param request body botapi.BotEditMessageRequest true "New content and components."
// @Success 200 {object} core.Message "The updated message."
// @Failure 400 {object} httpx.DiscordError "Invalid form body (50035)."
// @Failure 401 {object} httpx.DiscordError "Missing or invalid token (0)."
// @Failure 403 {object} httpx.DiscordError "Missing Permissions (50013), or not the bot's own message."
// @Failure 404 {object} httpx.DiscordError "Unknown Channel (10003) or Unknown Message (10008)."
// @Failure 429 {object} httpx.DiscordRateLimit "Rate limited; see retry_after and Retry-After."
// @Failure 500 {object} httpx.DiscordError "Server failure."
// @Router /api/bot/v1/channels/{channelID}/messages/{messageID} [patch]
func (a *API) botEditMessage(w http.ResponseWriter, r *http.Request) error {
	msgID, ref, err := a.Messages.MessageInChannel(r)
	if err != nil {
		return err
	}
	user := auth.UserFrom(r.Context())
	ctx, err := a.editContext(r.Context(), user, ref.ChannelID, nil)
	if err != nil {
		return err
	}
	var req rawBotEdit
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	var e BotMessageEdit
	if req.Content != nil {
		content, err := core.ValidateContent(*req.Content, true)
		if err != nil {
			return err
		}
		e.Content = &content
	}
	if e.Components, err = DecodeComponents(req.Components); err != nil {
		return err
	}
	msg, err := a.applyEdit(ctx, msgID, user.ID, e)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, msg)
	return nil
}

// editContext requires Send messages of the bot in the channel and carries
// the mention policy of its sends (messages.BotMentionPolicy), so an edit
// never reaches more than a send does.
func (a *API) editContext(ctx context.Context, user *auth.User, ch uuid.UUID, am *messages.AllowedMentions) (context.Context, error) {
	snap, p, err := ChannelPermsOf(ctx, a.Guard.Perms, user, ch)
	if err != nil {
		return nil, err
	}
	if err := snap.Deny(user.Subject(), p, perm.SendMessages); err != nil {
		return nil, err
	}
	return mentions.WithMentionPolicy(ctx, messages.BotMentionPolicy(snap, ch, p, am)), nil
}

// EditBotMessage edits the bot's own message msgID of channel ch (an
// interaction's response or the message a component is on). The bot must be
// able to view and send in the channel, like for any of its edits; a message
// of another channel is unknown.
func (a *API) EditBotMessage(ctx context.Context, bot *auth.User, ch, msgID uuid.UUID, e BotMessageEdit) (*core.Message, error) {
	if err := a.ownMessageIn(ctx, bot, ch, msgID); err != nil {
		return nil, err
	}
	ctx, err := a.editContext(ctx, bot, ch, e.AllowedMentions)
	if err != nil {
		return nil, err
	}
	return a.applyEdit(ctx, msgID, bot.ID, e)
}

// DeleteBotMessage deletes the bot's own message msgID of channel ch (the
// response to an interaction), as DELETE does.
func (a *API) DeleteBotMessage(ctx context.Context, bot *auth.User, ch, msgID uuid.UUID) error {
	if err := a.ownMessageIn(ctx, bot, ch, msgID); err != nil {
		return err
	}
	return a.Messages.DeleteOwnMessage(ctx, bot, msgID)
}

// ownMessageIn requires that the bot can view the channel and that the
// message is in it: what MessageInChannel checks for a route.
func (a *API) ownMessageIn(ctx context.Context, bot *auth.User, ch, msgID uuid.UUID) error {
	_, p, err := ChannelPermsOf(ctx, a.Guard.Perms, bot, ch)
	if err != nil {
		return err
	}
	if !p.Has(perm.ViewChannel) {
		return perm.ErrChannelHidden
	}
	ref, err := core.LoadMessageRef(ctx, a.DB, msgID)
	if err != nil {
		return err
	}
	if ref.ChannelID != ch {
		return core.ErrMessageNotFound
	}
	return nil
}

func (a *API) applyEdit(ctx context.Context, msgID, botUser uuid.UUID, e BotMessageEdit) (*core.Message, error) {
	return a.Messages.EditBotMessage(ctx, msgID, botUser, messages.BotEdit{Content: e.Content, InTx: e.store(ctx)})
}

// store writes the embeds and components of the edit in its transaction and
// refuses a message that would show nothing.
func (e BotMessageEdit) store(ctx context.Context) func(pgx.Tx, uuid.UUID) error {
	return func(tx pgx.Tx, id uuid.UUID) error {
		if e.Embeds != nil {
			if err := storeEmbeds(ctx, tx, id, *e.Embeds); err != nil {
				return err
			}
		}
		if e.Components != nil {
			if err := core.SaveComponents(ctx, tx, id, *e.Components); err != nil {
				return err
			}
		}
		return requireShows(ctx, tx, id)
	}
}

func storeEmbeds(ctx context.Context, tx pgx.Tx, id uuid.UUID, embeds []core.Embed) error {
	raw := []byte("[]")
	if len(embeds) > 0 {
		var err error
		if raw, err = json.Marshal(embeds); err != nil {
			return fmt.Errorf("marshal embeds: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE messages SET embeds = $1 WHERE id = $2`, raw, id); err != nil {
		return fmt.Errorf("update embeds: %w", err)
	}
	return nil
}

// requireShows refuses an edit that leaves a message with nothing to show:
// no text, embed, attachment, component or forward.
func requireShows(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	var shows bool
	if err := tx.QueryRow(ctx, `
		SELECT m.content <> '' OR m.embeds <> '[]'::jsonb OR m.forwarded
		       OR EXISTS (SELECT 1 FROM media WHERE message_id = m.id)
		       OR EXISTS (SELECT 1 FROM message_components WHERE message_id = m.id)
		FROM messages m WHERE m.id = $1`, id).Scan(&shows); err != nil {
		return fmt.Errorf("check edited message: %w", err)
	}
	if !shows {
		_, err := core.ValidateContent("", false)
		return err
	}
	return nil
}
