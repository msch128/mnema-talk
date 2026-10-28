package chat

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// ThreadFollowChangesPerMinute bounds follow and unfollow per member.
const ThreadFollowChangesPerMinute = 60

// maxFollowedThreads bounds the list of followed threads a client loads.
const maxFollowedThreads = 500

// ThreadFollow is the caller's follow state of one thread.
type ThreadFollow struct {
	// ThreadID is the root message of the thread.
	ThreadID uuid.UUID `json:"thread_id" format:"uuid"`
	// Following: replies of the thread notify the caller.
	Following bool `json:"following"`
}

// FollowedThreads lists the threads the caller follows, newest follow first.
type FollowedThreads struct {
	ThreadIDs []uuid.UUID `json:"thread_ids" format:"uuid"`
}

// replyFollowersSQL selects the people a reply concerns, as column u: the
// root's author, the replier and everyone mentioned by name or role. Its
// parameters are the thread ($1), the replier ($2), the reply ($3) and
// MentionKindGroup ($4), which is left out.
const replyFollowersSQL = `
		SELECT user_id AS u FROM messages WHERE id = $1
		UNION ALL SELECT $2::uuid
		UNION ALL SELECT user_id FROM message_mentions WHERE message_id = $3 AND kind <> $4`

// followOnReply makes the people a reply concerns follow its thread: the
// root's author, the replier and everyone mentioned by name or role. A member
// who unfollowed explicitly stays unfollowed. It runs in the reply's
// transaction, after its mentions are stored.
func followOnReply(ctx context.Context, tx pgx.Tx, replyID, threadID, replier uuid.UUID) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO thread_follows (user_id, thread_id)
		SELECT DISTINCT u, $1::uuid FROM (`+replyFollowersSQL+`
		) people
		ON CONFLICT (user_id, thread_id) DO NOTHING`, threadID, replier, replyID, MentionKindGroup)
	if err != nil {
		return fmt.Errorf("follow thread: %w", err)
	}
	return nil
}

// SetThreadFollow stores whether the user follows the thread. Following again
// counts as a new follow: it moves the thread to the front of the list.
func SetThreadFollow(ctx context.Context, q db.Querier, userID, threadID uuid.UUID, following bool) error {
	_, err := q.Exec(ctx, `
		INSERT INTO thread_follows (user_id, thread_id, following) VALUES ($1, $2, $3)
		ON CONFLICT (user_id, thread_id) DO UPDATE SET following = EXCLUDED.following,
			created_at = CASE WHEN EXCLUDED.following AND NOT thread_follows.following THEN NOW() ELSE thread_follows.created_at END`,
		userID, threadID, following)
	if err != nil {
		return fmt.Errorf("set thread follow: %w", err)
	}
	return nil
}

// IsFollowingThread reports the follow state of one thread.
func IsFollowingThread(ctx context.Context, q db.Querier, userID, threadID uuid.UUID) (bool, error) {
	var following bool
	err := q.QueryRow(ctx, `SELECT COALESCE((SELECT following FROM thread_follows WHERE user_id = $1 AND thread_id = $2), FALSE)`,
		userID, threadID).Scan(&following)
	if err != nil {
		return false, fmt.Errorf("thread follow state: %w", err)
	}
	return following, nil
}

// ThreadFollowers returns who follows a thread.
func ThreadFollowers(ctx context.Context, q db.Querier, threadID uuid.UUID) (map[uuid.UUID]bool, error) {
	rows, err := q.Query(ctx, `SELECT user_id FROM thread_follows WHERE thread_id = $1 AND following`, threadID)
	if err != nil {
		return nil, fmt.Errorf("thread followers: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, fmt.Errorf("thread followers: %w", err)
	}
	out := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out, nil
}

// putThreadFollow handles PUT /api/threads/{threadID}/follow.
//
// @Summary Follow a thread
// @Description Replies of the thread notify the caller from now on (push and in-page notifications). The root's author, everyone who replies and everyone mentioned in a reply follow automatically. Needs View channels; a reply or an unknown message is 404. Idempotent. Sends thread_follow_updated to the caller's sessions.
// @ID followThread
// @Tags Messages
// @Produce json
// @Security cookieAuth
// @Param threadID path string true "ID of the thread's root message." Format(uuid)
// @Success 200 {object} ThreadFollow "The follow state."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed ID."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED)."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: no such thread, or its channel is hidden from the caller."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/threads/{threadID}/follow [put]
func (h *Handler) putThreadFollow(w http.ResponseWriter, r *http.Request) error {
	return h.changeThreadFollow(w, r, true)
}

// deleteThreadFollow handles DELETE /api/threads/{threadID}/follow.
//
// @Summary Unfollow a thread
// @Description Replies of the thread stop notifying the caller, except those that mention the caller or quote their message. The choice is kept: a later reply by the caller does not follow the thread again. Needs View channels. Idempotent. Sends thread_follow_updated to the caller's sessions.
// @ID unfollowThread
// @Tags Messages
// @Produce json
// @Security cookieAuth
// @Param threadID path string true "ID of the thread's root message." Format(uuid)
// @Success 200 {object} ThreadFollow "The follow state."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed ID."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED)."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: no such thread, or its channel is hidden from the caller."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/threads/{threadID}/follow [delete]
func (h *Handler) deleteThreadFollow(w http.ResponseWriter, r *http.Request) error {
	return h.changeThreadFollow(w, r, false)
}

func (h *Handler) changeThreadFollow(w http.ResponseWriter, r *http.Request, following bool) error {
	id, err := httpx.PathUUID(r, "threadID")
	if err != nil {
		return err
	}
	root, err := h.loadThreadRoot(r, id)
	if err != nil {
		return err
	}
	user := auth.UserFrom(r.Context())
	if err := SetThreadFollow(r.Context(), h.DB, user.ID, id, following); err != nil {
		return err
	}
	if h.Events != nil {
		h.Events.SendToUsers([]uuid.UUID{user.ID}, events.ThreadFollowUpdated, map[string]any{
			"thread_id": id, "channel_id": root.ChannelID, "following": following,
		})
	}
	httpx.WriteJSON(w, http.StatusOK, ThreadFollow{ThreadID: id, Following: following})
	return nil
}

// listFollowedThreads handles GET /api/threads/following.
//
// @Summary Threads the caller follows
// @Description The root message IDs of the threads the caller follows in channels they may view, newest follow first, at most 500. A client uses it to decide which thread replies notify.
// @ID listFollowedThreads
// @Tags Messages
// @Produce json
// @Security cookieAuth
// @Success 200 {object} FollowedThreads "Followed threads."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED)."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/threads/following [get]
func (h *Handler) listFollowedThreads(w http.ResponseWriter, r *http.Request) error {
	visible, err := h.viewableChannels(r)
	if err != nil {
		return err
	}
	rows, err := h.DB.Query(r.Context(), `
		SELECT f.thread_id FROM thread_follows f JOIN messages m ON m.id = f.thread_id
		WHERE f.user_id = $1 AND f.following AND ($2::uuid[] IS NULL OR m.channel_id = ANY($2::uuid[]))
		ORDER BY f.created_at DESC, f.thread_id LIMIT $3`, auth.UserFrom(r.Context()).ID, visible, maxFollowedThreads)
	if err != nil {
		return fmt.Errorf("list followed threads: %w", err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return fmt.Errorf("list followed threads: %w", err)
	}
	if ids == nil {
		ids = []uuid.UUID{}
	}
	httpx.WriteJSON(w, http.StatusOK, FollowedThreads{ThreadIDs: ids})
	return nil
}
