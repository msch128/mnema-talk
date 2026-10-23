package chat

import (
	"context"
	"errors"
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
)

// Blocking is private to the blocker. Nothing the blocked member can read
// (message payloads, member lists, events, error texts) depends on it; the
// only places it shows are what the blocker is told: their own block list, and
// no inbox entry, mention count or push for the blocked member's mentions and
// replies. It is evaluated when a notification is delivered or listed, so it
// also applies to messages that already exist (doc/moderation.md).

// MaxBlocksPerMember bounds one member's block list.
const MaxBlocksPerMember = 500

// BlockChangesPerMinute is how many blocks and unblocks one member may make
// per minute.
const BlockChangesPerMinute = 30

var (
	errTooManyBlocks  = httpx.ErrConflict(fmt.Sprintf("you can block at most %d members; unblock someone first", MaxBlocksPerMember))
	errMemberNotFound = httpx.ErrNotFound("member not found")
	errBlockSelf      = httpx.ErrInvalidInput("you can't block yourself")
)

// BlockedMember is a member the caller has blocked.
type BlockedMember struct {
	UserID      uuid.UUID `json:"user_id" format:"uuid"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name"`
	AvatarURL   string    `json:"avatar_url,omitempty" binding:"optional"`
	BlockedAt   time.Time `json:"blocked_at" format:"date-time"`
}

// BlockUpdated is the payload of the block_updated event, sent to the
// blocker's own sessions only.
type BlockUpdated struct {
	UserID  uuid.UUID `json:"user_id" format:"uuid"`
	Blocked bool      `json:"blocked"`
}

// ListBlocks returns the members blocker has blocked, newest first.
func ListBlocks(ctx context.Context, q db.Querier, blocker uuid.UUID) ([]BlockedMember, error) {
	rows, err := q.Query(ctx, `
		SELECT u.id, u.username, u.display_name, u.avatar_s3_key, b.created_at
		FROM user_blocks b JOIN users u ON u.id = b.blocked_id
		WHERE b.blocker_id = $1 ORDER BY b.created_at DESC, u.id`, blocker)
	if err != nil {
		return nil, fmt.Errorf("list blocks: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (BlockedMember, error) {
		var m BlockedMember
		var avatar *string
		err := row.Scan(&m.UserID, &m.Username, &m.DisplayName, &avatar, &m.BlockedAt)
		m.AvatarURL = auth.AvatarURL(avatar)
		return m, err
	})
	if err != nil {
		return nil, fmt.Errorf("list blocks: %w", err)
	}
	if out == nil {
		out = []BlockedMember{}
	}
	return out, nil
}

// AddBlock records that blocker blocked target; blocking someone twice changes
// nothing. It reports whether a block was added.
func AddBlock(ctx context.Context, p *db.Pool, blocker, target uuid.UUID) (bool, error) {
	if blocker == target {
		return false, errBlockSelf
	}
	added := false
	err := pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		// Serialize one member's block changes so the cap holds.
		if _, err := tx.Exec(ctx, `SELECT 1 FROM users WHERE id = $1 FOR NO KEY UPDATE`, blocker); err != nil {
			return err
		}
		var exists, found bool
		var n int
		err := tx.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM user_blocks WHERE blocker_id = $1 AND blocked_id = $2),
			       EXISTS (SELECT 1 FROM users WHERE id = $2 AND deleted_at IS NULL),
			       (SELECT COUNT(*) FROM user_blocks WHERE blocker_id = $1)`, blocker, target).Scan(&exists, &found, &n)
		if err != nil {
			return err
		}
		switch {
		case exists:
			return nil
		case !found:
			return errMemberNotFound
		case n >= MaxBlocksPerMember:
			return errTooManyBlocks
		}
		_, err = tx.Exec(ctx, `INSERT INTO user_blocks (blocker_id, blocked_id) VALUES ($1, $2)`, blocker, target)
		added = err == nil
		return err
	})
	return added, err
}

// RemoveBlock lifts a block; it reports whether there was one.
func RemoveBlock(ctx context.Context, q db.Querier, blocker, target uuid.UUID) (bool, error) {
	tag, err := q.Exec(ctx, `DELETE FROM user_blocks WHERE blocker_id = $1 AND blocked_id = $2`, blocker, target)
	if err != nil {
		return false, fmt.Errorf("remove block: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// BlockedEitherWay reports whether one of the two members has blocked the
// other. Callers must not let the answer reach either of them (see the direct
// message rules in doc/architecture.md).
func BlockedEitherWay(ctx context.Context, q db.Querier, a, b uuid.UUID) (bool, error) {
	var blocked bool
	err := q.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM user_blocks
		               WHERE (blocker_id = $1 AND blocked_id = $2) OR (blocker_id = $2 AND blocked_id = $1))`, a, b).Scan(&blocked)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return blocked, err
}

func (h *Handler) mountBlocks(r chi.Router) {
	r.Get("/me/blocks", httpx.Handle(h.listBlocks))
	changes := r
	if h.BlockLimit != nil {
		changes = r.With(h.BlockLimit.By(auth.UserKey))
	}
	changes.Put("/me/blocks/{userID}", httpx.Handle(h.blockMember))
	changes.Delete("/me/blocks/{userID}", httpx.Handle(h.unblockMember))
}

// listBlocks handles GET /api/me/blocks.
//
// @Summary List blocked members
// @Description The members the caller has blocked, newest first. Blocking is private: the blocked member is never told, and nothing they can read depends on it. A blocked member's mentions and replies create no inbox entry, mention count or push for the caller; clients collapse their messages.
// @ID listBlocks
// @Tags Users
// @Produce json
// @Security cookieAuth
// @Success 200 {array} BlockedMember "The caller's blocked members."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/me/blocks [get]
func (h *Handler) listBlocks(w http.ResponseWriter, r *http.Request) error {
	out, err := ListBlocks(r.Context(), h.DB, auth.UserFrom(r.Context()).ID)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}

// blockMember handles PUT /api/me/blocks/{userID}.
//
// @Summary Block a member
// @Description Idempotent. At most 500 blocks per member. The blocked member is not told and cannot detect the block through this API; direct messages between the two members stop working with a generic error. Broadcasts block_updated to the caller's own sessions.
// @ID blockMember
// @Tags Users
// @Produce json
// @Security cookieAuth
// @Param userID path string true "Member ID." Format(uuid)
// @Success 204 "Blocked (or already blocked)."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT: the block list is full."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/me/blocks/{userID} [put]
func (h *Handler) blockMember(w http.ResponseWriter, r *http.Request) error {
	return h.changeBlock(w, r, true)
}

// unblockMember handles DELETE /api/me/blocks/{userID}.
//
// @Summary Unblock a member
// @Description Idempotent. Broadcasts block_updated to the caller's own sessions.
// @ID unblockMember
// @Tags Users
// @Produce json
// @Security cookieAuth
// @Param userID path string true "Member ID." Format(uuid)
// @Success 204 "Unblocked (or was not blocked)."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/me/blocks/{userID} [delete]
func (h *Handler) unblockMember(w http.ResponseWriter, r *http.Request) error {
	return h.changeBlock(w, r, false)
}

func (h *Handler) changeBlock(w http.ResponseWriter, r *http.Request, block bool) error {
	target, err := httpx.PathUUID(r, "userID")
	if err != nil {
		return err
	}
	me := auth.UserFrom(r.Context()).ID
	var changed bool
	if block {
		changed, err = AddBlock(r.Context(), h.DB, me, target)
	} else {
		changed, err = RemoveBlock(r.Context(), h.DB, me, target)
	}
	if err != nil {
		return err
	}
	if changed && h.Events != nil {
		h.Events.SendToUsers([]uuid.UUID{me}, events.BlockUpdated, BlockUpdated{UserID: target, Blocked: block})
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
