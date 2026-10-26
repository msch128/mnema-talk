package moderation

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// Limits of the notes.
const (
	MaxNoteLen          = 1000
	MaxNotesPerMember   = 100
	deletedAuthorName   = "Deleted member"
	noteColumnsWithName = `n.id, n.author_id, COALESCE(a.display_name, ''), n.body, n.strike, n.created_at, n.updated_at`
)

var errNoteNotFound = httpx.ErrNotFound("note not found")

// Note is a private moderator note about a member. The member never sees it
// and it is not part of their data export.
type Note struct {
	ID         uuid.UUID `json:"id" format:"uuid"`
	AuthorID   uuid.UUID `json:"author_id" format:"uuid"`
	AuthorName string    `json:"author_name"`
	Body       string    `json:"body" minLength:"1" maxLength:"1000"`
	// Strike counts the note in the member's strikes.
	Strike    bool      `json:"strike"`
	CreatedAt time.Time `json:"created_at" format:"date-time"`
	UpdatedAt time.Time `json:"updated_at" format:"date-time"`
	// Mine: the caller wrote it and may edit and delete it.
	Mine bool `json:"mine"`
	// CanDelete: the caller wrote it, or is an Administrator.
	CanDelete bool `json:"can_delete"`
}

// NoteRequest is the body that adds or edits a note.
type NoteRequest struct {
	Body   string `json:"body" minLength:"1" maxLength:"1000"`
	Strike bool   `json:"strike"`
}

func (n *Note) scan(row pgx.CollectableRow) error {
	return row.Scan(&n.ID, &n.AuthorID, &n.AuthorName, &n.Body, &n.Strike, &n.CreatedAt, &n.UpdatedAt)
}

// listNotes returns the notes about a member, newest first, with what the
// caller may do with each.
func (h *Handler) listNotes(ctx context.Context, subject uuid.UUID, actor *auth.User) ([]Note, error) {
	rows, err := h.DB.Query(ctx, `
		SELECT `+noteColumnsWithName+` FROM moderation_notes n LEFT JOIN users a ON a.id = n.author_id
		WHERE n.subject_id = $1 ORDER BY n.created_at DESC, n.id DESC LIMIT $2`, subject, MaxNotesPerMember)
	if err != nil {
		return nil, fmt.Errorf("list notes: %w", err)
	}
	notes, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Note, error) {
		var n Note
		return n, n.scan(row)
	})
	if err != nil {
		return nil, fmt.Errorf("list notes: %w", err)
	}
	admin, err := h.isAdministrator(ctx, actor)
	if err != nil {
		return nil, err
	}
	for i := range notes {
		notes[i].Mine = notes[i].AuthorID == actor.ID
		notes[i].CanDelete = notes[i].Mine || admin
		if notes[i].AuthorName == "" {
			notes[i].AuthorName = deletedAuthorName
		}
	}
	if notes == nil {
		notes = []Note{}
	}
	return notes, nil
}

func (h *Handler) isAdministrator(ctx context.Context, u *auth.User) (bool, error) {
	if u.IsAdmin() {
		return true, nil
	}
	have, err := h.Perms.Global(ctx, u.Subject())
	return have.Has(perm.Administrator), err
}

func readNote(r *http.Request) (NoteRequest, error) {
	var req NoteRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return req, err
	}
	body, err := httpx.CleanText("body", req.Body, MaxNoteLen, true)
	req.Body = body
	return req, err
}

// addNote handles POST /api/admin/users/{id}/moderation/notes.
//
// @Summary Add a moderator note
// @Description Writes a private note about a member, optionally counted as a strike. Only moderators with Manage reports see it; the member never does, and it is not in their data export. At most 100 notes per member. Recorded in the audit log as moderation.note_add (without the text).
// @ID addModerationNote
// @Tags Moderation
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param id path string true "User ID." Format(uuid)
// @Param request body NoteRequest true "Request body."
// @Success 201 {object} Note "The note."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Manage reports permission, or the member is yourself."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT: the member already has 100 notes."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/users/{id}/moderation/notes [post]
func (h *Handler) addNote(w http.ResponseWriter, r *http.Request) error {
	subject, err := h.subject(r)
	if err != nil {
		return err
	}
	req, err := readNote(r)
	if err != nil {
		return err
	}
	actor := auth.UserFrom(r.Context())
	var noteID uuid.UUID
	err = pgx.BeginFunc(r.Context(), h.DB, func(tx pgx.Tx) error {
		// The member's row serializes writers, so the cap holds.
		if _, err := tx.Exec(r.Context(), `SELECT 1 FROM users WHERE id = $1 FOR NO KEY UPDATE`, subject); err != nil {
			return err
		}
		var n int
		if err := tx.QueryRow(r.Context(), `SELECT COUNT(*) FROM moderation_notes WHERE subject_id = $1`, subject).Scan(&n); err != nil {
			return err
		}
		if n >= MaxNotesPerMember {
			return httpx.ErrConflict(fmt.Sprintf("a member can have %d notes at most; delete an old one", MaxNotesPerMember))
		}
		return tx.QueryRow(r.Context(), `
			INSERT INTO moderation_notes (subject_id, author_id, body, strike) VALUES ($1, $2, $3, $4) RETURNING id`,
			subject, actor.ID, req.Body, req.Strike).Scan(&noteID)
	})
	if err != nil {
		return err
	}
	note, err := h.loadNote(r.Context(), subject, noteID, actor)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, note)
	return nil
}

func (h *Handler) loadNote(ctx context.Context, subject, id uuid.UUID, actor *auth.User) (Note, error) {
	var n Note
	rows, err := h.DB.Query(ctx, `
		SELECT `+noteColumnsWithName+` FROM moderation_notes n LEFT JOIN users a ON a.id = n.author_id
		WHERE n.id = $1 AND n.subject_id = $2`, id, subject)
	if err != nil {
		return n, err
	}
	n, err = pgx.CollectExactlyOneRow(rows, func(row pgx.CollectableRow) (Note, error) {
		var n Note
		return n, n.scan(row)
	})
	if isNoRows(err) {
		return n, errNoteNotFound
	}
	if err != nil {
		return n, err
	}
	admin, err := h.isAdministrator(ctx, actor)
	if err != nil {
		return n, err
	}
	n.Mine = n.AuthorID == actor.ID
	n.CanDelete = n.Mine || admin
	if n.AuthorName == "" {
		n.AuthorName = deletedAuthorName
	}
	return n, nil
}

// editNote handles PUT /api/admin/users/{id}/moderation/notes/{noteID}.
//
// @Summary Edit your moderator note
// @Description Changes the text or the strike flag of a note you wrote. Nobody edits another moderator's note. Needs Manage reports. Recorded in the audit log as moderation.note_edit (without the text).
// @ID editModerationNote
// @Tags Moderation
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param id path string true "User ID." Format(uuid)
// @Param noteID path string true "Note ID." Format(uuid)
// @Param request body NoteRequest true "Request body."
// @Success 200 {object} Note "The note."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Manage reports permission, or the note is another moderator's."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/users/{id}/moderation/notes/{noteID} [put]
func (h *Handler) editNote(w http.ResponseWriter, r *http.Request) error {
	subject, err := h.subject(r)
	if err != nil {
		return err
	}
	noteID, err := httpx.PathUUID(r, "noteID")
	if err != nil {
		return err
	}
	req, err := readNote(r)
	if err != nil {
		return err
	}
	actor := auth.UserFrom(r.Context())
	existing, err := h.loadNote(r.Context(), subject, noteID, actor)
	if err != nil {
		return err
	}
	if !existing.Mine {
		return httpx.ErrForbidden("you can only edit your own notes")
	}
	if _, err := h.DB.Exec(r.Context(), `
		UPDATE moderation_notes SET body = $3, strike = $4, updated_at = NOW() WHERE id = $1 AND subject_id = $2`,
		noteID, subject, req.Body, req.Strike); err != nil {
		return fmt.Errorf("edit note: %w", err)
	}
	note, err := h.loadNote(r.Context(), subject, noteID, actor)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, note)
	return nil
}

// deleteNote handles DELETE /api/admin/users/{id}/moderation/notes/{noteID}.
//
// @Summary Delete a moderator note
// @Description Deletes a note you wrote; an Administrator (or the Owner) may delete any note. Needs Manage reports. Recorded in the audit log as moderation.note_delete.
// @ID deleteModerationNote
// @Tags Moderation
// @Security cookieAuth
// @Param id path string true "User ID." Format(uuid)
// @Param noteID path string true "Note ID." Format(uuid)
// @Success 204 "Deleted."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Manage reports permission, or the note is another moderator's and you are no Administrator."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/users/{id}/moderation/notes/{noteID} [delete]
func (h *Handler) deleteNote(w http.ResponseWriter, r *http.Request) error {
	subject, err := h.subject(r)
	if err != nil {
		return err
	}
	noteID, err := httpx.PathUUID(r, "noteID")
	if err != nil {
		return err
	}
	existing, err := h.loadNote(r.Context(), subject, noteID, auth.UserFrom(r.Context()))
	if err != nil {
		return err
	}
	if !existing.CanDelete {
		return httpx.ErrForbidden("you can only delete your own notes")
	}
	if _, err := h.DB.Exec(r.Context(), `DELETE FROM moderation_notes WHERE id = $1 AND subject_id = $2`, noteID, subject); err != nil {
		return fmt.Errorf("delete note: %w", err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
