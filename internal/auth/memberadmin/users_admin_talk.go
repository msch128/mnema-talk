package memberadmin

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/msch128/mnema-talk/internal/auth"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// kickUser handles POST /api/admin/users/{id}/kick.
//
// @Summary Remove a user from voice
// @Description 400 when the user is not in a Talk the caller may moderate (View channels and Disconnect members in that Talk; a Talk they may not moderate answers the same). Requires the Disconnect members permission (403 otherwise); members whose highest role is not below the caller's cannot be targeted, and the Owner cannot be removed (400).
// @ID kickUserFromVoice
// @Tags Admin
// @Produce json
// @Security cookieAuth
// @Param id path string true "User ID." Format(uuid)
// @Success 204 "Success, no content."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Disconnect members permission, or the member's highest role is not below yours, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/users/{id}/kick [post]
func (h *API) kickUser(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	// The target check and the kick run under the permission lock, like every
	// other moderation action, so a role change cannot slip in between.
	kicked := false
	err = h.Moderated(r, id, func(db.Querier) (err error) {
		kicked, err = h.kickUserFromVoice(r, id, uuid.Nil)
		return err
	})
	return h.finishTalkKick(w, err, kicked)
}

// finishTalkKick maps the outcome of a Talk kick to the response.
func (h *API) finishTalkKick(w http.ResponseWriter, err error, kicked bool) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.ErrNotFound("user not found")
	}
	if err != nil {
		return err
	}
	if !kicked {
		return httpx.ErrInvalidInput("user is not in a Talk")
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// disconnectFromTalk handles POST /api/channels/{id}/voice/{userID}/disconnect.
//
// @Summary Disconnect a member from a Talk
// @Description Needs View channel and Disconnect members in this Talk: granted server-wide by a role, it works in every Talk; granted or denied by a role or member overwrite on the Talk or its category, only there. Members whose highest role is not below the caller's cannot be targeted, and the Owner cannot be removed (400). 400 also when the member is not in this Talk. Recorded in the audit log as voice.disconnect, read only by auditors who can view the Talk; a removal from a conversation's call Talk is not recorded.
// @ID disconnectFromTalk
// @Tags Channels
// @Produce json
// @Security cookieAuth
// @Param id path string true "Talk ID." Format(uuid)
// @Param userID path string true "User ID." Format(uuid)
// @Success 204 "Success, no content."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): bad IDs, or the member is not in this Talk."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Disconnect members permission in this Talk, or the member's highest role is not below yours, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the Talk or the user does not exist, or the Talk is hidden from the caller."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/channels/{id}/voice/{userID}/disconnect [post]
func (h *API) disconnectFromTalk(w http.ResponseWriter, r *http.Request) error {
	ch, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	id, err := httpx.PathUUID(r, "userID")
	if err != nil {
		return err
	}
	kicked, audited := false, false
	err = h.Moderated(r, id, func(db.Querier) (err error) {
		// Refused before anything is said about who sits in the Talk.
		if err := h.Perms.RequireChannel(r.Context(), auth.UserFrom(r.Context()).Subject(), ch, perm.ViewChannel|perm.DisconnectMembers); err != nil {
			return err
		}
		// Decided before the removal, while the Talk exists: a call Talk
		// left empty may be deleted by the time the entry is written.
		audited = auditableTalk(r.Context(), h.Sessions.DB, ch)
		kicked, err = h.kickUserFromVoice(r, id, ch)
		return err
	})
	if err == nil && kicked && audited {
		h.auditTalkRemoval(r, id, ch)
	}
	return h.finishTalkKick(w, err, kicked)
}

// auditableTalk says whether a removal from the Talk may be audited. A
// conversation's call Talk is not audited at all, not even for the Owner
// (doc/talks.md: who called whom). A Talk that is gone may have been a
// conversation's, and one whose kind cannot be told may be one: neither is
// audited (fail closed, as for voice.join; see talks.IsDMTalk).
func auditableTalk(ctx context.Context, q db.Querier, ch uuid.UUID) bool {
	var public bool
	err := q.QueryRow(ctx, `SELECT dm_id IS NULL FROM channels WHERE id = $1`, ch).Scan(&public)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		slog.Warn("audit log: Talk removal not recorded, DM lookup failed", "channel", ch, "err", err)
	}
	return err == nil && public
}

// auditTalkRemoval records a removal from one Talk. Outside the admin area, so
// the audit middleware does not see it. It says who was in which Talk, so it
// is a voice.* entry with its Talk as the channel: auditors who cannot view
// the Talk do not read it, like voice.join (H-L6-03); user.talk_kick is the
// admin-wide kick's. The caller asks auditableTalk first.
func (h *API) auditTalkRemoval(r *http.Request, id, ch uuid.UUID) {
	ctx := r.Context()
	e := audit.Entry{Action: "voice.disconnect", TargetType: "user", TargetID: id.String(), ChannelID: &ch, Details: map[string]any{"channel_id": ch.String()}}
	if u, err := auth.GetUser(ctx, h.Sessions.DB, id); err == nil {
		e.TargetName = u.Username
	}
	h.Audit.RecordOrLog(ctx, e)
}

// kickUserFromVoice removes id from the Talks the caller may moderate; with
// only set, from that Talk alone.
func (h *API) kickUserFromVoice(r *http.Request, id, only uuid.UUID) (bool, error) {
	if h.Live == nil {
		return false, nil
	}
	actor := auth.UserFrom(r.Context()).Subject()
	kicked := false
	for _, ch := range h.Live.VoiceChannels(id) {
		if only != uuid.Nil && ch != only {
			continue
		}
		allowed, err := h.canModerateVoiceChannel(r, actor, ch)
		if err != nil {
			return false, err
		}
		if !allowed {
			continue
		}
		h.Live.KickFromChannel(id, ch)
		kicked = true
	}
	return kicked, nil
}

func (h *API) canModerateVoiceChannel(r *http.Request, actor perm.Subject, ch uuid.UUID) (bool, error) {
	err := h.Perms.RequireChannel(r.Context(), actor, ch, perm.ViewChannel|perm.DisconnectMembers)
	if err == nil {
		return true, nil
	}
	if _, refused := httpx.AsAPIError(err); !refused {
		return false, err
	}
	return false, nil
}

// MountAuthenticated registers the one voice-admin route that lives beside the
// Talk routes, not under /admin: it needs a session, and the permission is
// checked per Talk.
func (h *API) MountAuthenticated(r chi.Router) {
	r.Post("/channels/{id}/voice/{userID}/disconnect", httpx.Handle(h.disconnectFromTalk))
}
