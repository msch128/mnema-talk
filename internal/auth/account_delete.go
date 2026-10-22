package auth

import (
	"context"
	"io"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// ObjectStore is the part of the object store the account flows use (uploads
// to remove after a deletion, the export archive). media.Store satisfies it.
type ObjectStore interface {
	Upload(ctx context.Context, key string, body io.Reader, mimeType string, size int64) error
	GetObjectFrom(ctx context.Context, key string, offset int64) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
	DeleteBatch(ctx context.Context, keys []string) error
}

// errOwnerDelete explains why the one Owner account has no delete button.
var errOwnerDelete = httpx.ErrForbidden("the Owner account cannot be deleted: ownership is not transferable, so the community would be left without an Owner")

// DeleteAccountRequest is the body of POST /api/users/me/delete.
type DeleteAccountRequest struct {
	CurrentPassword string `json:"current_password" format:"password"`
	// Code is a current authenticator code or an unused recovery code; needed
	// only while two-factor sign-in is on.
	Code string `json:"code,omitempty" maxLength:"32" binding:"optional"`
}

// deleteAccount handles POST /api/users/me/delete.
//
// @Summary Delete my account
// @Description Erases the caller's personal data (GDPR Art. 17) and cannot be undone. Needs the current password and, while two-factor sign-in is on, a current code; wrong answers count toward the usual lockouts. The Owner account cannot be deleted (403). Profile, avatar, e-mail address, sessions, tokens, reactions, uploads (files too), invites, settings and roles are deleted. The account's messages stay readable as written by "Deleted user" but lose their text, link cards and pins, so threads keep their shape; replies and quotes by other members stay. Audit entries about the account lose names and details. The username becomes free. The request signs the caller out everywhere.
// @ID deleteAccount
// @Tags Users
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param request body DeleteAccountRequest true "Request body."
// @Success 204 "Account deleted."
// @Header 204 {string} Set-Cookie "Expired session cookie that clears the session."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: wrong password or code, the Owner account, or cross-origin request rejected."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests, or the second step is locked."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/users/me/delete [post]
func (h *Handler) deleteAccount(w http.ResponseWriter, r *http.Request) error {
	u := UserFrom(r.Context())
	var req DeleteAccountRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if u.IsAdmin() {
		return errOwnerDelete
	}
	if ok, err := h.ConfirmIdentity(w, r, u, req.CurrentPassword, req.Code); !ok {
		return err
	}
	var keys []string
	err := pgx.BeginFunc(r.Context(), h.Sessions.DB, func(tx pgx.Tx) (err error) {
		keys, err = eraseAccount(r.Context(), tx, u.ID)
		return err
	})
	if err != nil {
		return err
	}
	h.removeObjects(r.Context(), keys)
	h.endDeletedAccount(r.Context(), u)
	h.Sessions.End(w)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// removeObjects deletes the account's files from the object store. The rows
// are gone already, so a failure leaves orphans the Owner's orphan cleanup
// finds; it is logged without the keys.
func (h *Handler) removeObjects(ctx context.Context, keys []string) {
	if len(keys) == 0 || h.Objects == nil {
		return
	}
	if err := h.Objects.DeleteBatch(ctx, keys); err != nil {
		slog.Warn("files of a deleted account not removed", "files", len(keys), "err", err)
	}
}

// endDeletedAccount ends what is live for the account and records the
// deletion. The entry names the account by its id only.
func (h *Handler) endDeletedAccount(ctx context.Context, u *User) {
	if h.Live != nil {
		h.Live.KickFromVoice(u.ID)
		h.Live.DisconnectUser(u.ID)
	}
	if h.OnAccountEnded != nil {
		h.OnAccountEnded(u.ID)
	}
	if h.Events != nil {
		h.Events.Broadcast(events.UserUpdate, map[string]any{"id": u.ID, "disabled": true})
	}
	if h.Audit == nil {
		return
	}
	id := u.ID
	h.Audit.RecordOrLog(ctx, audit.Entry{
		ActorID: &id, ActorName: "", Action: "auth.account_delete",
		TargetType: "user", TargetID: id.String(), TargetName: "",
	})
}
