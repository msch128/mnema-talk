package account

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"time"

	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/events/wire"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// deleteCleanupTimeout bounds what follows the commit of an account deletion:
// removing its files and recording the deletion.
const deleteCleanupTimeout = 30 * time.Second

// errOwnerDelete explains why the one Owner account has no delete button.
var errOwnerDelete = httpx.ErrForbidden("the Owner account cannot be deleted: ownership is not transferable, so the community would be left without an Owner")

// deleteAccount handles POST /api/users/me/delete.
//
// @Summary Delete my account
// @Description Erases the caller's personal data (GDPR Art. 17) and cannot be undone. Needs the current password and, while two-factor sign-in is on, a current code; wrong answers count toward the usual lockouts. The Owner account cannot be deleted (403). Profile, avatar, e-mail address, sessions, tokens, reactions, uploads (files too), invites, settings and roles are deleted. The account's messages stay readable as written by "Deleted member" but lose their text, link cards and pins, so threads keep their shape; replies and quotes by other members stay. Audit entries about the account lose names and details. The username becomes free. The request signs the caller out everywhere.
// @ID deleteAccount
// @Tags Users
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param request body auth.DeleteAccountRequest true "Request body."
// @Success 204 "Account deleted."
// @Header 204 {string} Set-Cookie "Expired session cookie that clears the session."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: wrong password or code, the Owner account, or cross-origin request rejected."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests, or the second step is locked."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Failure 503 {object} httpx.ErrorResponse "UNAVAILABLE: the server is busy (the erasure kept meeting other writes of the account and was rolled back); nothing was deleted, try again shortly."
// @Router /api/users/me/delete [post]
func (h *API) deleteAccount(w http.ResponseWriter, r *http.Request) error {
	u := auth.UserFrom(r.Context())
	var req auth.DeleteAccountRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if u.IsAdmin() {
		return errOwnerDelete
	}
	if ok, err := h.ConfirmIdentity(w, r, u, req.CurrentPassword, req.Code); !ok {
		return err
	}
	gone, err := h.eraseInTransaction(r.Context(), u.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		// A parallel request erased the account while this one waited for it.
		h.Sessions.End(w)
		w.WriteHeader(http.StatusNoContent)
		return nil
	}
	if err != nil {
		return err
	}
	// The rows are gone and cannot be retried: a client that hangs up now must
	// not leave files behind or the deletion unrecorded.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), deleteCleanupTimeout)
	defer cancel()
	h.removeObjects(ctx, gone.keys)
	h.announceUnpins(u, gone.unpins)
	h.endDeletedAccount(ctx, u)
	h.Sessions.End(w)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// eraseAttempts bounds how often the erasure runs when PostgreSQL rolls it
// back to break a deadlock.
const eraseAttempts = 3

// eraseInTransaction erases the account and records it, in one transaction.
// The erasure locks the users row first and the member's messages,
// memberships and Talks later. A write that locked one of those rows first
// (the member's edit from a second device, their leave of a group, a pin) and
// waits for a row the erasure holds closes a cycle, and PostgreSQL may roll
// the erasure back (H-L10-06). It then runs again from the start: it is one
// transaction, and nothing outside it happens before the commit (files,
// events and sign-outs follow in the caller). After the last attempt the
// member is asked to try again (503).
func (h *API) eraseInTransaction(rctx context.Context, id uuid.UUID) (*erased, error) {
	// Erasing touches every row the member ever wrote.
	ctx := db.WithStatementTimeout(rctx, db.BulkStatementTimeout)
	for attempt := 1; ; attempt++ {
		var gone *erased
		err := pgx.BeginFunc(ctx, h.Sessions.DB, func(tx pgx.Tx) (err error) {
			if gone, err = h.eraseOwnAccount(ctx, tx, id); err != nil {
				return err
			}
			// In the erasure's transaction: an erased account always has its entry.
			return h.Audit.RecordTx(ctx, tx, audit.Entry{
				ActorID: &id, ActorName: "", Action: "auth.account_delete",
				TargetType: "user", TargetID: id.String(), TargetName: "",
			})
		})
		if !db.IsDeadlock(err) {
			return gone, err
		}
		if attempt == eraseAttempts {
			slog.Warn("account erasure rolled back by deadlocks; the member is asked to try again", "attempts", attempt, "err", err)
			return nil, auth.ErrServerBusy
		}
		// A short jittered pause lets the other transaction finish first.
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(20+rand.IntN(80)) * time.Millisecond):
		}
	}
}

// removeObjects deletes the account's files from the object store. The rows
// are gone already, so a failure leaves orphans the Owner's orphan cleanup
// finds; it is logged without the keys.
func (h *API) removeObjects(ctx context.Context, keys []string) {
	if len(keys) == 0 || h.Objects == nil {
		return
	}
	if err := h.Objects.DeleteBatch(ctx, keys); err != nil {
		slog.Warn("files of a deleted account not removed", "files", len(keys), "err", err)
	}
}

// announceUnpins tells the channels that the erasure unpinned messages, so
// open pin panels refetch. The actor is the deleted account, without a name.
func (h *API) announceUnpins(u *auth.User, unpins []pinRef) {
	if h.Events == nil {
		return
	}
	for _, p := range unpins {
		h.Events.BroadcastChannel(p.channelID, wire.PinRemoved{ChannelID: p.channelID, MessageID: p.messageID, ActorID: u.ID})
	}
}

// endDeletedAccount ends what is live for the account. (Its audit entry,
// which names the account by its id only, was written with the erasure.)
func (h *API) endDeletedAccount(ctx context.Context, u *auth.User) {
	if h.Live != nil {
		h.Live.KickFromVoice(u.ID)
		h.Live.DisconnectUser(u.ID)
	}
	if h.OnAccountEnded != nil {
		h.OnAccountEnded(u.ID)
	}
	if h.Events != nil {
		h.Events.Broadcast(wire.UserDisabled{ID: u.ID, Disabled: true})
	}
}
