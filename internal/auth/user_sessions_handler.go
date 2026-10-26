package auth

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// MemberSession is one of the caller's own sessions on this server.
type MemberSession struct {
	ID uuid.UUID `json:"id" format:"uuid"`
	// Device is a coarse label ("Firefox · Linux") parsed from the user
	// agent, empty when it is not recognised. No address and no raw user
	// agent are stored.
	Device    string    `json:"device" maxLength:"64"`
	CreatedAt time.Time `json:"created_at" format:"date-time"`
	// LastActiveAt is when the session last made an authenticated request,
	// accurate to a few minutes.
	LastActiveAt time.Time `json:"last_active_at" format:"date-time"`
	// Current marks the session this request was made with.
	Current bool `json:"current"`
	// Push tells whether the session's device receives Web Push notifications.
	Push bool `json:"push"`
}

// MemberSessionList is the response of GET /api/auth/sessions.
type MemberSessionList struct {
	Sessions []MemberSession `json:"sessions"`
}

// RevokedSessions is the response of POST /api/auth/sessions/revoke-others.
type RevokedSessions struct {
	Revoked int `json:"revoked" minimum:"0"`
}

// liveSession is the SQL condition for a user_sessions row that still works:
// not expired, from the member's current token version, not signed out and
// not inactive for too long.
const liveSession = `expires_at > NOW()
	AND token_version = (SELECT token_version FROM users WHERE id = user_sessions.user_id)
	AND NOT EXISTS (SELECT 1 FROM revoked_sessions WHERE jti = user_sessions.jti)
	AND NOT (` + inactiveSQL + `)`

// listSessions handles GET /api/auth/sessions.
//
// @Summary List my sessions
// @Description The caller's own sessions on this server, most recently active first: a coarse device label, when each was created and last used, whether it is the one making this request and whether its device receives push notifications. Sessions of other members are never listed.
// @ID listMySessions
// @Tags Auth
// @Produce json
// @Security cookieAuth
// @Success 200 {object} MemberSessionList "Sessions."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/auth/sessions [get]
func (h *Handler) listSessions(w http.ResponseWriter, r *http.Request) error {
	u := UserFrom(r.Context())
	current, _ := h.Sessions.SessionClaims(r)
	rows, err := h.Sessions.DB.Query(r.Context(), `
		SELECT id, jti, device, created_at, last_active_at,
		       EXISTS (SELECT 1 FROM push_subscriptions p WHERE p.session_id = user_sessions.jti)
		FROM user_sessions WHERE user_id = $1 AND `+liveSession+`
		ORDER BY last_active_at DESC, id`, u.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	out := MemberSessionList{Sessions: []MemberSession{}}
	for rows.Next() {
		var s MemberSession
		var jti string
		if err := rows.Scan(&s.ID, &jti, &s.Device, &s.CreatedAt, &s.LastActiveAt, &s.Push); err != nil {
			return err
		}
		s.Current = jti == current
		out.Sessions = append(out.Sessions, s)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}

// revokeSession handles DELETE /api/auth/sessions/{id}.
//
// @Summary Sign out one of my sessions
// @Description Ends one of the caller's own sessions and removes its push subscriptions; its open WebSockets are closed. Signing out the session the request was made with is allowed and clears the cookie. A session that belongs to someone else answers 404 like one that does not exist.
// @ID revokeMySession
// @Tags Auth
// @Produce json
// @Security cookieAuth
// @Param id path string true "Session ID from the list." Format(uuid)
// @Success 204 "Session ended."
// @Header 204 {string} Set-Cookie "Expired session cookie when the current session was ended."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/auth/sessions/{id} [delete]
func (h *Handler) revokeSession(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	u := UserFrom(r.Context())
	current, _ := h.Sessions.SessionClaims(r)
	jtis, err := h.endOwnSessions(r.Context(), u.ID, &id, "")
	if err != nil {
		return err
	}
	if len(jtis) == 0 {
		return httpx.ErrNotFound("session not found")
	}
	h.auditUser(r.Context(), u, "auth.session_revoke")
	h.disconnect(jtis)
	for _, jti := range jtis {
		if jti == current {
			h.Sessions.End(w)
		}
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// revokeOtherSessions handles POST /api/auth/sessions/revoke-others.
//
// @Summary Sign out all my other sessions
// @Description Ends every session of the caller except the one making the request, and removes their push subscriptions. A session that has not made a request since the 1.2 upgrade is not listed yet and is not covered; "Sign out everywhere" (POST /api/auth/logout-all) ends those too.
// @ID revokeOtherSessions
// @Tags Auth
// @Produce json
// @Security cookieAuth
// @Success 200 {object} RevokedSessions "How many sessions ended."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: cross-origin request rejected."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/auth/sessions/revoke-others [post]
func (h *Handler) revokeOtherSessions(w http.ResponseWriter, r *http.Request) error {
	u := UserFrom(r.Context())
	current, _ := h.Sessions.SessionClaims(r)
	jtis, err := h.endOwnSessions(r.Context(), u.ID, nil, current)
	if err != nil {
		return err
	}
	if len(jtis) > 0 {
		h.auditUser(r.Context(), u, "auth.sessions_revoke_others")
	}
	h.disconnect(jtis)
	httpx.WriteJSON(w, http.StatusOK, RevokedSessions{Revoked: len(jtis)})
	return nil
}

// endOwnSessions signs out the user's own live sessions: the one with the
// given list id, or all except the session id keep. It returns the ended
// session ids. Rows of other members can never match: every statement is
// scoped by user_id.
func (h *Handler) endOwnSessions(ctx context.Context, user uuid.UUID, id *uuid.UUID, keep string) ([]string, error) {
	var jtis []string
	var expires []time.Time
	tx, err := h.Sessions.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `DELETE FROM user_sessions
		WHERE user_id = $1 AND ($2::uuid IS NULL OR id = $2) AND jti <> $3 AND `+liveSession+`
		RETURNING jti, expires_at`, user, id, keep)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var jti string
		var exp time.Time
		if err := rows.Scan(&jti, &exp); err != nil {
			rows.Close()
			return nil, err
		}
		jtis, expires = append(jtis, jti), append(expires, exp)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := endSessions(ctx, tx, jtis, expires); err != nil {
		return nil, err
	}
	return jtis, tx.Commit(ctx)
}

// disconnect closes the open sockets of the ended sessions.
func (h *Handler) disconnect(jtis []string) {
	if h.Live == nil {
		return
	}
	for _, jti := range jtis {
		h.Live.DisconnectSession(jti)
	}
}
