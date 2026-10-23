package chat

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// Saved messages are a private list per member. A row is a reference and the
// member's own note, never a copy: what the list shows of a message is decided
// by the member's access when the list is read, so a message the member can no
// longer see appears as "unavailable" (its id and the member's own note only).

// MaxSavedPerMember bounds one member's saved list.
const MaxSavedPerMember = 1000

// MaxSavedNoteLen is the longest note on a saved message, in characters.
const MaxSavedNoteLen = 200

// SavedChangesPerMinute is how many saves, note edits and removals one member
// may make per minute.
const SavedChangesPerMinute = 60

var errTooManySaved = httpx.ErrConflict(fmt.Sprintf("you can save at most %d messages; remove one first", MaxSavedPerMember))

// SavedMessage is one entry of the caller's saved list.
type SavedMessage struct {
	MessageID uuid.UUID `json:"message_id" format:"uuid"`
	// Note is the caller's own note (up to 200 characters), possibly empty.
	Note    string    `json:"note" maxLength:"200"`
	SavedAt time.Time `json:"saved_at" format:"date-time"`
	// Available is false when the caller may no longer see the message; then
	// Message is absent and nothing but the id and the note is sent.
	Available bool     `json:"available"`
	Message   *Message `json:"message,omitempty" binding:"optional"`
}

// SavedPage is one page of the saved list, newest first.
type SavedPage struct {
	Items   []SavedMessage `json:"items"`
	HasMore bool           `json:"has_more"`
}

// SavedUpdated is the payload of the saved_updated event, sent to the saver's
// own sessions only.
type SavedUpdated struct {
	MessageID uuid.UUID `json:"message_id" format:"uuid"`
	Saved     bool      `json:"saved"`
}

// SaveRequest is the optional body of PUT /api/me/saved/{messageID}.
type SaveRequest struct {
	// Note replaces the note; left out (or null), a saved message keeps its
	// note and a new one starts without.
	Note *string `json:"note" binding:"optional" maxLength:"200" extensions:"x-nullable"`
}

func (h *Handler) mountSaved(r chi.Router) {
	r.Get("/me/saved", httpx.Handle(h.listSaved))
	changes := r
	if h.SavedLimit != nil {
		changes = r.With(h.SavedLimit.By(auth.UserKey))
	}
	changes.Put("/me/saved/{messageID}", httpx.Handle(h.saveMessage))
	changes.Delete("/me/saved/{messageID}", httpx.Handle(h.unsaveMessage))
}

type savedRow struct {
	id      uuid.UUID
	note    string
	savedAt time.Time
}

// ListSaved returns one page of user's saved list, newest first. visible is the
// set of channels the user may view now (nil means all); a saved message in any
// other channel is returned as unavailable. before is the message id of the
// last entry of the previous page.
func ListSaved(ctx context.Context, p *db.Pool, user uuid.UUID, visible []uuid.UUID, before *uuid.UUID, limit int) (SavedPage, error) {
	rows, err := p.Query(ctx, `
		SELECT s.message_id, s.note, s.created_at FROM saved_messages s
		WHERE s.user_id = $1 AND ($3::uuid IS NULL OR (s.created_at, s.message_id) <
			(SELECT created_at, message_id FROM saved_messages WHERE user_id = $1 AND message_id = $3))
		ORDER BY s.created_at DESC, s.message_id DESC LIMIT $2`, user, limit+1, before)
	if err != nil {
		return SavedPage{}, fmt.Errorf("list saved: %w", err)
	}
	saved, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (savedRow, error) {
		var s savedRow
		return s, row.Scan(&s.id, &s.note, &s.savedAt)
	})
	if err != nil {
		return SavedPage{}, fmt.Errorf("list saved: %w", err)
	}
	page := SavedPage{Items: make([]SavedMessage, 0, len(saved))}
	if len(saved) > limit {
		saved, page.HasMore = saved[:limit], true
	}
	byID, err := savedMessages(ctx, p, user, saved, visible)
	if err != nil {
		return SavedPage{}, err
	}
	for _, s := range saved {
		e := SavedMessage{MessageID: s.id, Note: s.note, SavedAt: s.savedAt}
		if m, ok := byID[s.id]; ok {
			e.Available, e.Message = true, m
		}
		page.Items = append(page.Items, e)
	}
	return page, nil
}

// savedMessages loads the messages of saved that the user may see.
func savedMessages(ctx context.Context, p *db.Pool, user uuid.UUID, saved []savedRow, visible []uuid.UUID) (map[uuid.UUID]*Message, error) {
	ids := make([]uuid.UUID, len(saved))
	for i, s := range saved {
		ids[i] = s.id
	}
	// A conversation is readable by its members only, whatever the permission
	// state says (the Owner and Administrators included), so that rule is a
	// membership test in SQL, as in search.
	msgs, err := queryMessages(ctx, p, messageSelect+` WHERE m.id = ANY($1) AND (
		(NOT m.in_dm AND ($2::uuid[] IS NULL OR m.channel_id = ANY($2)))
		OR (m.in_dm AND EXISTS (SELECT 1 FROM dm_members dm WHERE dm.channel_id = m.channel_id AND dm.user_id = $3)))`, ids, visible, user)
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]*Message, len(msgs))
	for i := range msgs {
		out[msgs[i].ID] = &msgs[i]
	}
	return out, nil
}

// SaveMessage saves messageID for user, or updates the note of an existing
// entry (a nil note keeps the existing one). It returns the entry as stored.
func SaveMessage(ctx context.Context, p *db.Pool, user, messageID uuid.UUID, note *string) (SavedMessage, error) {
	out := SavedMessage{MessageID: messageID}
	err := pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		// Serialize one member's changes so the cap holds.
		if _, err := tx.Exec(ctx, `SELECT 1 FROM users WHERE id = $1 FOR NO KEY UPDATE`, user); err != nil {
			return err
		}
		var exists bool
		var n int
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM saved_messages WHERE user_id = $1 AND message_id = $2),
			       (SELECT COUNT(*) FROM saved_messages WHERE user_id = $1)`, user, messageID).Scan(&exists, &n); err != nil {
			return err
		}
		if !exists && n >= MaxSavedPerMember {
			return errTooManySaved
		}
		return tx.QueryRow(ctx, `
			INSERT INTO saved_messages (user_id, message_id, note) VALUES ($1, $2, COALESCE($3, ''))
			ON CONFLICT (user_id, message_id) DO UPDATE SET note = COALESCE($3, saved_messages.note)
			RETURNING created_at, note`, user, messageID, note).Scan(&out.SavedAt, &out.Note)
	})
	return out, err
}

// listSaved handles GET /api/me/saved.
//
// @Summary List saved messages
// @Description The caller's private saved list, newest first. Visibility is checked on every read: a message the caller can no longer see comes back with available=false and no message, only their own note. At most 1,000 messages can be saved.
// @ID listSaved
// @Tags Messages
// @Produce json
// @Security cookieAuth
// @Param limit query int false "Page size. Values below 1 or non-numeric fall back to the default; larger values are clamped to 100." minimum(1) maximum(100) default(50)
// @Param before query string false "Page back: only entries older than this saved message." Format(uuid)
// @Success 200 {object} SavedPage "One page of the saved list."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/me/saved [get]
func (h *Handler) listSaved(w http.ResponseWriter, r *http.Request) error {
	var before *uuid.UUID
	if _, err := queryIDs(r, "a message id", map[string]**uuid.UUID{"before": &before}); err != nil {
		return err
	}
	visible, err := h.viewableChannels(r)
	if err != nil {
		return err
	}
	page, err := ListSaved(r.Context(), h.DB, auth.UserFrom(r.Context()).ID, visible, before, httpx.QueryLimit(r, "limit", 50, 100))
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, page)
	return nil
}

// saveMessage handles PUT /api/me/saved/{messageID}.
//
// @Summary Save a message
// @Description Adds the message to the caller's private saved list, or changes the note of a saved one (idempotent; a body without note keeps the note). Needs View channel for the message's channel; otherwise the message does not exist for the caller (404). At most 1,000 saved messages (409 beyond). Sends saved_updated to the caller's own sessions.
// @ID saveMessage
// @Tags Messages
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param messageID path string true "Message ID." Format(uuid)
// @Param request body SaveRequest false "Optional note."
// @Success 200 {object} SavedMessage "The saved entry."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT: the saved list is full."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/me/saved/{messageID} [put]
func (h *Handler) saveMessage(w http.ResponseWriter, r *http.Request) error {
	msgID, err := httpx.PathUUID(r, "messageID")
	if err != nil {
		return err
	}
	var req SaveRequest
	if r.ContentLength != 0 {
		if err := httpx.DecodeJSON(r, &req); err != nil {
			return err
		}
	}
	if req.Note != nil {
		note, err := httpx.CleanText("note", *req.Note, MaxSavedNoteLen, false)
		if err != nil {
			return err
		}
		req.Note = &note
	}
	ref, err := loadMessageRef(r.Context(), h.DB, msgID)
	if err != nil {
		return err
	}
	if err := h.require(r, ref.ChannelID, perm.ViewChannel); err != nil {
		return notFoundUnlessFailure(err)
	}
	me := auth.UserFrom(r.Context()).ID
	entry, err := SaveMessage(r.Context(), h.DB, me, msgID, req.Note)
	if err != nil {
		return err
	}
	if entry.Message, err = GetMessage(r.Context(), h.DB, msgID); err != nil {
		return err
	}
	entry.Available = true
	h.sendSaved(me, msgID, true)
	httpx.WriteJSON(w, http.StatusOK, entry)
	return nil
}

// unsaveMessage handles DELETE /api/me/saved/{messageID}.
//
// @Summary Remove a saved message
// @Description Idempotent. Works for a message the caller can no longer see. Sends saved_updated to the caller's own sessions.
// @ID unsaveMessage
// @Tags Messages
// @Produce json
// @Security cookieAuth
// @Param messageID path string true "Message ID." Format(uuid)
// @Success 204 "Removed (or was not saved)."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/me/saved/{messageID} [delete]
func (h *Handler) unsaveMessage(w http.ResponseWriter, r *http.Request) error {
	msgID, err := httpx.PathUUID(r, "messageID")
	if err != nil {
		return err
	}
	me := auth.UserFrom(r.Context()).ID
	tag, err := h.DB.Exec(r.Context(), `DELETE FROM saved_messages WHERE user_id = $1 AND message_id = $2`, me, msgID)
	if err != nil {
		return fmt.Errorf("remove saved: %w", err)
	}
	if tag.RowsAffected() == 1 {
		h.sendSaved(me, msgID, false)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (h *Handler) sendSaved(user, msgID uuid.UUID, saved bool) {
	if h.Events != nil {
		h.Events.SendToUsers([]uuid.UUID{user}, events.SavedUpdated, SavedUpdated{MessageID: msgID, Saved: saved})
	}
}
