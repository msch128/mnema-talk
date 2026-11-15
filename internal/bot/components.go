package bot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/chat/messages"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// ComponentUse is the body of using a component: a click on a button or a
// choice in a select menu.
type ComponentUse struct {
	CustomID string `json:"custom_id" maxLength:"100"`
	// Values are what a select menu got: option values of a string select,
	// member, role or channel IDs of the others. Left out for a button.
	Values []string `json:"values" binding:"optional" maxItems:"25"`
}

// errComponentGone answers a click on a component that is gone, disabled or
// whose bot can no longer take it (disabled, deleted, the channel hidden from
// it): the same 404 for all of them.
var errComponentGone = httpx.ErrNotFound("component not found")

// clickGate is what every component use and modal submit passes: the member
// can view the channel (404 otherwise, like a hidden channel), has accepted
// the rules, is not timed out, and is within the limit of 30 a minute.
func (c *Commands) clickGate(w http.ResponseWriter, r *http.Request, user *auth.User) (uuid.UUID, *perm.Snapshot, error) {
	ch, err := httpx.PathUUID(r, "channelID")
	if err != nil {
		return uuid.Nil, nil, err
	}
	snap, err := c.Perms.ForChannel(r.Context(), ch)
	if err != nil {
		return uuid.Nil, nil, err
	}
	if !snap.Channel(user.Subject(), ch).Has(perm.ViewChannel) {
		return uuid.Nil, nil, perm.ErrChannelHidden
	}
	if snap.RulesPending(user.ID) {
		return uuid.Nil, nil, perm.ErrRulesNotAccepted
	}
	if _, timedOut := snap.Timeout(user.ID); timedOut {
		return uuid.Nil, nil, httpx.ErrForbidden("you are timed out")
	}
	if ok, retry := c.clicks.Allow(user.ID.String()); !ok {
		w.Header().Set("Retry-After", fmt.Sprint(int(retry.Seconds())+1))
		return uuid.Nil, nil, httpx.ErrRateLimited()
	}
	return ch, snap, nil
}

// componentMessage is a bot's message with components, as a click needs it.
type componentMessage struct {
	botID, botUser uuid.UUID
	botName        string
	avatar         *string
	components     []core.ActionRow
}

// loadComponentMessage reads the message msgID of channel ch with its
// components and its bot, which must be enabled, active and able to view the
// channel. A message of another channel is unknown, like a missing one.
func (c *Commands) loadComponentMessage(ctx context.Context, snap *perm.Snapshot, ch, msgID uuid.UUID) (*componentMessage, error) {
	var m componentMessage
	var raw []byte
	var usable bool
	err := c.DB.QueryRow(ctx, `
		SELECT b.id, b.user_id, b.name, u.avatar_s3_key, mc.components, NOT b.disabled AND `+auth.ActiveSQL("u")+`
		FROM messages msg
		JOIN message_components mc ON mc.message_id = msg.id
		JOIN bots b ON b.user_id = msg.user_id
		JOIN users u ON u.id = b.user_id
		WHERE msg.id = $1 AND msg.channel_id = $2`, msgID, ch).Scan(&m.botID, &m.botUser, &m.botName, &m.avatar, &raw, &usable)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, core.ErrMessageNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load component message: %w", err)
	}
	if !usable || !snap.Channel(perm.Subject{ID: m.botUser}, ch).Has(perm.ViewChannel) {
		return nil, errComponentGone
	}
	if err := json.Unmarshal(raw, &m.components); err != nil {
		return nil, fmt.Errorf("decode components: %w", err)
	}
	return &m, nil
}

// usableComponent finds the enabled component with customID.
func usableComponent(rows []core.ActionRow, customID string) (core.Component, error) {
	comp, ok := core.FindComponent(rows, customID)
	if !ok || comp.Disabled {
		return core.Component{}, errComponentGone
	}
	return comp, nil
}

// useMessageComponent handles POST /api/channels/{channelID}/messages/{messageID}/components.
//
// @Summary Use a component of a bot's message
// @Description A click on a button or a choice in a select menu of a bot's message. Needs View channels; a timed-out member cannot. The message must still have that enabled component, select values must fit it (options of a string select, between min_values and max_values; members who can view the channel, roles that exist, channels the member and the bot can view). Creates a new interaction (type 3) and delivers it to the bot. At most 30 clicks and modal submits a minute per member.
// @ID useMessageComponent
// @Tags Commands
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param channelID path string true "Channel ID." Format(uuid)
// @Param messageID path string true "Message ID." Format(uuid)
// @Param body body ComponentUse true "The component and the chosen values."
// @Success 202 {object} Invoked "Delivered to the bot."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): values that do not fit the component."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED)."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: the member is timed out, or cross-origin request rejected. RULES_NOT_ACCEPTED: the member has not accepted the community rules yet."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: no such channel or message, or it is hidden from the caller; the component is gone or disabled, or its bot is disabled, deleted or cannot view the channel."
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT: the bot is not connected."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/channels/{channelID}/messages/{messageID}/components [post]
func (c *Commands) useMessageComponent(w http.ResponseWriter, r *http.Request) error {
	user := auth.UserFrom(r.Context())
	ch, snap, err := c.clickGate(w, r, user)
	if err != nil {
		return err
	}
	msgID, err := httpx.PathUUID(r, "messageID")
	if err != nil {
		return err
	}
	var req ComponentUse
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	cm, err := c.loadComponentMessage(r.Context(), snap, ch, msgID)
	if err != nil {
		return err
	}
	comp, err := usableComponent(cm.components, req.CustomID)
	if err != nil {
		return err
	}
	data, err := c.componentData(r.Context(), snap, user, ch, cm.botUser, comp, req.Values)
	if err != nil {
		return err
	}
	// The message as the bot API shows it, read now: a message deleted since
	// is not delivered.
	msg, err := messages.GetMessage(r.Context(), c.DB, msgID)
	if err != nil {
		return err
	}
	in := &interaction{kind: InteractionMessageComponent, botID: cm.botID, botUser: cm.botUser, botName: cm.botName,
		avatar: auth.AvatarURL(cm.avatar), channel: ch, invoker: user.ID, source: source{message: msgID}}
	return c.startInteraction(w, in, user, snap.Channel(user.Subject(), ch), data, msg)
}

// useNoteComponent handles POST /api/channels/{channelID}/ephemeral/{noteID}/components.
//
// @Summary Use a component of an ephemeral note
// @Description Like using a component of a message, for a bot's ephemeral response (interaction_response with components). Only the member who sees the note, for the 15 minutes of the interaction that made it; afterwards, or after a restart of the server, 404. The bot receives message {id, flags: 64, content, components}.
// @ID useNoteComponent
// @Tags Commands
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param channelID path string true "Channel ID." Format(uuid)
// @Param noteID path string true "Note ID (id of interaction_response)." Format(uuid)
// @Param body body ComponentUse true "The component and the chosen values."
// @Success 202 {object} Invoked "Delivered to the bot."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): values that do not fit the component."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED)."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: the member is timed out, or cross-origin request rejected. RULES_NOT_ACCEPTED: the member has not accepted the community rules yet."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: no such channel or note, it is not the caller's or has expired; the component is gone or disabled, or its bot can no longer take it."
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT: the bot is not connected."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/channels/{channelID}/ephemeral/{noteID}/components [post]
func (c *Commands) useNoteComponent(w http.ResponseWriter, r *http.Request) error {
	user := auth.UserFrom(r.Context())
	ch, snap, err := c.clickGate(w, r, user)
	if err != nil {
		return err
	}
	noteID, err := httpx.PathUUID(r, "noteID")
	if err != nil {
		return err
	}
	var req ComponentUse
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	n, ok := c.store.noteFor(noteID, user.ID, ch)
	if !ok {
		return httpx.ErrNotFound("note not found")
	}
	name, avatar, err := c.usableBot(r.Context(), snap, n.botID, n.botUser, ch)
	if err != nil {
		return err
	}
	comp, err := usableComponent(n.components, req.CustomID)
	if err != nil {
		return err
	}
	data, err := c.componentData(r.Context(), snap, user, ch, n.botUser, comp, req.Values)
	if err != nil {
		return err
	}
	in := &interaction{kind: InteractionMessageComponent, botID: n.botID, botUser: n.botUser, botName: name,
		avatar: avatar, channel: ch, invoker: user.ID, source: source{note: n.id}}
	return c.startInteraction(w, in, user, snap.Channel(user.Subject(), ch), data, notePayload(n))
}

// notePayload is a note as the bot receives it in an interaction's message.
func notePayload(n note) map[string]any {
	return map[string]any{"id": n.id, "flags": flagEphemeral, "content": n.content, "components": n.components}
}

// usableBot checks that the bot is enabled, its account active and that it
// can view the channel, and returns its name and avatar.
func (c *Commands) usableBot(ctx context.Context, snap *perm.Snapshot, botID, botUser, ch uuid.UUID) (string, string, error) {
	var name string
	var avatar *string
	var usable bool
	err := c.DB.QueryRow(ctx, `
		SELECT b.name, u.avatar_s3_key, NOT b.disabled AND `+auth.ActiveSQL("u")+`
		FROM bots b JOIN users u ON u.id = b.user_id WHERE b.id = $1`, botID).Scan(&name, &avatar, &usable)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", errComponentGone
	}
	if err != nil {
		return "", "", fmt.Errorf("load bot: %w", err)
	}
	if !usable || !snap.Channel(perm.Subject{ID: botUser}, ch).Has(perm.ViewChannel) {
		return "", "", errComponentGone
	}
	return name, auth.AvatarURL(avatar), nil
}

// startInteraction registers a new interaction of a component use or a
// modal submit and delivers it: 202, 409 when the bot is not connected, 429
// when the store's caps for the bot or the member are full.
func (c *Commands) startInteraction(w http.ResponseWriter, in *interaction, user *auth.User, perms perm.Set, data, message any) error {
	id := uuid.New()
	token, ok := c.store.add(id, in)
	if !ok {
		return httpx.ErrRateLimited()
	}
	d := map[string]any{
		"id": id, "token": token, "type": in.kind, "channel_id": in.channel, "application_id": in.botID,
		"data": data, "member": memberPayload(user, perms),
	}
	if message != nil {
		d["message"] = message
	}
	payload, err := json.Marshal(d)
	if err != nil {
		c.store.remove(id)
		return err
	}
	if err := c.deliverOrRollback(in.botID, id, payload); err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusAccepted, Invoked{InteractionID: id})
	return nil
}

// memberPayload is the member who caused an interaction, as the bot receives it.
func memberPayload(user *auth.User, perms perm.Set) map[string]any {
	return map[string]any{
		"user":        map[string]any{"id": user.ID, "username": user.Username, "display_name": user.DisplayName, "avatar_url": user.AvatarURL},
		"permissions": strconv.FormatUint(uint64(perms), 10),
	}
}
