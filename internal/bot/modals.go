package bot

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/chat/messages"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// ModalSubmit is the body of submitting a modal.
type ModalSubmit struct {
	// Values maps the custom_id of each text input to what the member typed;
	// an input left out is not filled in.
	Values map[string]string `json:"values"`
}

var errModalGone = httpx.ErrNotFound("modal not found")

// submitModal handles POST /api/channels/{channelID}/modals/{modalID}.
//
// @Summary Submit a modal
// @Description Sends the answers of a modal a bot opened (interaction_modal) to the bot as an interaction of type 5. Only the member it was opened for, once, within 15 minutes; after a restart of the server an open modal is gone (404). The answers are trimmed and must fit their inputs (required, min_length, max_length). Needs View channels; a timed-out member cannot. At most 30 clicks and modal submits a minute per member. When the bot is not connected (409) the modal can be submitted again.
// @ID submitModal
// @Tags Commands
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param channelID path string true "Channel ID." Format(uuid)
// @Param modalID path string true "Modal ID (id of interaction_modal)." Format(uuid)
// @Param body body ModalSubmit true "The answers."
// @Success 202 {object} Invoked "Delivered to the bot."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): an unknown input, a required one left out, or an answer of the wrong length."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED)."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: the member is timed out, or cross-origin request rejected. RULES_NOT_ACCEPTED: the member has not accepted the community rules yet."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: no such channel or modal, it was opened for someone else, already submitted or expired; or its bot can no longer take it."
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT: the bot is not connected."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/channels/{channelID}/modals/{modalID} [post]
func (c *Commands) submitModal(w http.ResponseWriter, r *http.Request) error {
	user := auth.UserFrom(r.Context())
	ch, snap, err := c.clickGate(w, r, user)
	if err != nil {
		return err
	}
	modalID, err := httpx.PathUUID(r, "modalID")
	if err != nil {
		return err
	}
	var req ModalSubmit
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	m, ok := c.store.takeModal(modalID, user.ID, ch)
	if !ok {
		return errModalGone
	}
	if err := c.deliverSubmit(w, r, snap, user, m, req.Values); err != nil {
		c.store.giveBackModal(m)
		return err
	}
	c.store.closeModal(m.id)
	return nil
}

// deliverSubmit checks the answers and the bot, and delivers the submit.
func (c *Commands) deliverSubmit(w http.ResponseWriter, r *http.Request, snap *perm.Snapshot, user *auth.User, m *modal, values map[string]string) error {
	rows, err := modalAnswers(m.form, values)
	if err != nil {
		return err
	}
	name, avatar, err := c.usableBot(r.Context(), snap, m.botID, m.botUser, m.channel)
	if err != nil {
		return err
	}
	message, err := c.sourcePayload(r.Context(), m)
	if err != nil {
		return err
	}
	in := &interaction{kind: InteractionModalSubmit, botID: m.botID, botUser: m.botUser, botName: name,
		avatar: avatar, channel: m.channel, invoker: user.ID, source: m.source}
	data := map[string]any{"custom_id": m.form.CustomID, "components": rows}
	return c.startInteraction(w, in, user, snap.Channel(user.Subject(), m.channel), data, message)
}

// modalAnswers checks the answers against the modal's inputs and returns
// them in Discord's shape: one action row per text input, in the modal's
// order, with its custom_id and value.
func modalAnswers(form core.Modal, values map[string]string) ([]map[string]any, error) {
	known := map[string]bool{}
	rows := make([]map[string]any, 0, len(form.Components))
	for _, row := range form.Components {
		input := row.Components[0]
		known[input.CustomID] = true
		v, err := core.CheckInputAnswer(input, values[input.CustomID])
		if err != nil {
			return nil, err
		}
		rows = append(rows, map[string]any{"type": core.ComponentActionRow, "components": []map[string]any{
			{"type": core.ComponentTextInput, "custom_id": input.CustomID, "value": v},
		}})
	}
	for id := range values {
		if !known[id] {
			return nil, httpx.ErrInvalidInput("values: unknown input " + id)
		}
	}
	return rows, nil
}

// sourcePayload is the message of a modal opened from a component, as the
// bot receives it with the submit: the message as the bot API shows it now,
// or the note. A modal opened from a command has none (nil).
func (c *Commands) sourcePayload(ctx context.Context, m *modal) (any, error) {
	if m.source.message != uuid.Nil {
		msg, err := messages.GetMessage(ctx, c.DB, m.source.message)
		if err != nil {
			return nil, err
		}
		return msg, nil
	}
	if m.source.note != uuid.Nil {
		if n, ok := c.store.noteOf(m.source.note, m.botID); ok {
			return notePayload(n), nil
		}
		return map[string]any{"id": m.source.note, "flags": flagEphemeral}, nil
	}
	return nil, nil
}
