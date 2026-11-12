package messages

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// MaxPinsPerChannel is the most messages a channel keeps pinned.
const MaxPinsPerChannel = 50

// PinChangesPerMinute is how many pins and unpins one member may make per minute.
const PinChangesPerMinute = 60

var errTooManyPins = httpx.ErrConflict(fmt.Sprintf("a channel can keep at most %d pinned messages; unpin one first", MaxPinsPerChannel))

func (a *API) mountPins(r chi.Router) {
	r.Get("/channels/{channelID}/pins", httpx.Handle(a.listPins))
	changes := r
	if a.PinLimit != nil {
		changes = r.With(a.PinLimit.By(auth.UserKey))
	}
	changes.Put("/channels/{channelID}/messages/{messageID}/pin", httpx.Handle(a.pinMessage))
	changes.Delete("/channels/{channelID}/messages/{messageID}/pin", httpx.Handle(a.unpinMessage))
}

// PinsChanged is the payload of the pins_changed event: a message of the
// channel was pinned or unpinned. Clients refetch the pinned list and show a
// compact notice.
type PinsChanged struct {
	ChannelID uuid.UUID `json:"channel_id" format:"uuid"`
	MessageID uuid.UUID `json:"message_id" format:"uuid"`
	Pinned    bool      `json:"pinned"`
	ActorID   uuid.UUID `json:"actor_id" format:"uuid"`
	ActorName string    `json:"actor_name"`
	// PinnedAt and PinnedBy are set when the message was pinned.
	PinnedAt *time.Time    `json:"pinned_at,omitempty" binding:"optional" format:"date-time"`
	PinnedBy *core.UserRef `json:"pinned_by,omitempty" binding:"optional"`
}

func (PinsChanged) EventName() string { return events.PinsChanged }

// listPins handles GET /api/channels/{channelID}/pins.
//
// @Summary List a channel's pinned messages
// @Description The channel's pinned messages, the most recently pinned first (at most 50), each with pinned_at and pinned_by. Needs View channel. Deleting a message unpins it.
// @ID listPins
// @Tags Messages
// @Produce json
// @Security cookieAuth
// @Param channelID path string true "Channel ID." Format(uuid)
// @Success 200 {array} core.Message "The pinned messages."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/channels/{channelID}/pins [get]
func (a *API) listPins(w http.ResponseWriter, r *http.Request) error {
	chID, err := a.validateListChannel(r)
	if err != nil {
		return err
	}
	msgs, err := QueryMessages(r.Context(), a.DB, MessageSelect+`
		WHERE m.channel_id = $1 AND m.is_pinned
		ORDER BY m.pinned_at DESC NULLS LAST, m.id DESC
		LIMIT $2`, chID, MaxPinsPerChannel)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, msgs)
	return nil
}

// pinMessage handles PUT /api/channels/{channelID}/messages/{messageID}/pin.
//
// @Summary Pin a message
// @Description Needs the Manage messages permission in the channel (the Owner always). A channel keeps at most 50 pinned messages (409 beyond). Pinning a pinned message changes nothing. Broadcasts message_update and pins_changed.
// @ID pinMessage
// @Tags Messages
// @Produce json
// @Security cookieAuth
// @Param channelID path string true "Channel ID." Format(uuid)
// @Param messageID path string true "Message ID." Format(uuid)
// @Success 200 {object} core.Message "The message."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Manage messages permission in the channel, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT: the channel already has 50 pinned messages."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/channels/{channelID}/messages/{messageID}/pin [put]
func (a *API) pinMessage(w http.ResponseWriter, r *http.Request) error {
	return a.setPinned(w, r, true)
}

// unpinMessage handles DELETE /api/channels/{channelID}/messages/{messageID}/pin.
//
// @Summary Unpin a message
// @Description Needs the Manage messages permission in the channel (the Owner always). Unpinning a message that is not pinned changes nothing. Broadcasts message_update and pins_changed.
// @ID unpinMessage
// @Tags Messages
// @Produce json
// @Security cookieAuth
// @Param channelID path string true "Channel ID." Format(uuid)
// @Param messageID path string true "Message ID." Format(uuid)
// @Success 200 {object} core.Message "The message."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Manage messages permission in the channel, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/channels/{channelID}/messages/{messageID}/pin [delete]
func (a *API) unpinMessage(w http.ResponseWriter, r *http.Request) error {
	return a.setPinned(w, r, false)
}

func (a *API) setPinned(w http.ResponseWriter, r *http.Request, pinned bool) error {
	chID, err := httpx.PathUUID(r, "channelID")
	if err != nil {
		return err
	}
	msgID, err := httpx.PathUUID(r, "messageID")
	if err != nil {
		return err
	}
	actor := auth.UserFrom(r.Context())
	changed := false
	err = a.asModerator(r, chID, func(tx pgx.Tx, _ *perm.Snapshot) error {
		var err error
		changed, err = writePin(r, tx, pinTarget{channel: chID, message: msgID, by: actor.ID}, pinned)
		if err != nil || !changed {
			return err
		}
		return RecordMessageEvent(r.Context(), a.DB, tx, events.MessageUpdate, chID, msgID, nil)
	})
	if err != nil {
		return err
	}
	msg, err := GetMessage(r.Context(), a.DB, msgID)
	if err != nil {
		return err
	}
	if changed {
		a.announcePin(r, msg, actor, pinned)
	}
	httpx.WriteJSON(w, http.StatusOK, msg)
	return nil
}

type pinTarget struct{ channel, message, by uuid.UUID }

// writePin sets the pin state inside tx and reports whether it changed. The
// channel row is locked (without blocking message inserts) so two moderators
// cannot both take the last free slot.
func writePin(r *http.Request, tx pgx.Tx, t pinTarget, pinned bool) (bool, error) {
	ctx := r.Context()
	if _, err := tx.Exec(ctx, `SELECT 1 FROM channels WHERE id = $1 FOR NO KEY UPDATE`, t.channel); err != nil {
		return false, fmt.Errorf("lock channel: %w", err)
	}
	// A pinned reply keeps its thread root from auto-delete. Like a reply's
	// insert, the pin takes a key-share lock on the root before it changes the
	// reply: the sweep (FOR UPDATE SKIP LOCKED) skips the root while the pin
	// is in flight, and a pin that comes after the sweep locked the root waits
	// for it. parent_id never changes, so the subquery's snapshot is current.
	if _, err := tx.Exec(ctx, `
		SELECT 1 FROM messages WHERE id = (SELECT parent_id FROM messages WHERE id = $1 AND channel_id = $2)
		FOR KEY SHARE`, t.message, t.channel); err != nil {
		return false, fmt.Errorf("lock thread root: %w", err)
	}
	var current bool
	err := tx.QueryRow(ctx, `SELECT is_pinned FROM messages WHERE id = $1 AND channel_id = $2 FOR NO KEY UPDATE`, t.message, t.channel).Scan(&current)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, core.ErrMessageNotFound
	}
	if err != nil {
		return false, fmt.Errorf("load message: %w", err)
	}
	if current == pinned {
		return false, nil
	}
	if pinned {
		var count int
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM messages WHERE channel_id = $1 AND is_pinned`, t.channel).Scan(&count); err != nil {
			return false, fmt.Errorf("count pins: %w", err)
		}
		if count >= MaxPinsPerChannel {
			return false, errTooManyPins
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE messages SET is_pinned = $2,
			pinned_at = CASE WHEN $2 THEN NOW() END, pinned_by = CASE WHEN $2 THEN $3::uuid END
		WHERE id = $1`, t.message, pinned, t.by); err != nil {
		return false, fmt.Errorf("pin message: %w", err)
	}
	return true, nil
}

func (a *API) announcePin(r *http.Request, msg *core.Message, actor *auth.User, pinned bool) {
	action := "message.unpin"
	if pinned {
		action = "message.pin"
	}
	chID := msg.ChannelID
	a.Audit.RecordOrLog(r.Context(), audit.Entry{
		Action: action, TargetType: "message", TargetID: msg.ID.String(), ChannelID: &chID,
		Details: map[string]any{"channel_id": chID, "author_id": msg.UserID, "author_name": msg.Username, "content": audit.Content(msg.Content)},
	})
	if a.Events == nil {
		return
	}
	a.Events.BroadcastChannel(chID, (*core.MessageUpdate)(msg))
	a.Events.BroadcastChannel(chID, PinsChanged{
		ChannelID: chID, MessageID: msg.ID, Pinned: pinned, ActorID: actor.ID, ActorName: actor.DisplayName,
		PinnedAt: msg.PinnedAt, PinnedBy: msg.PinnedBy,
	})
}
