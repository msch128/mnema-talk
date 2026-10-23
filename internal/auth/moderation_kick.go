package auth

import (
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// kickDisconnectDelay is how long a kicked member's socket stays open, so the
// kicked event reaches them before the connection closes.
const kickDisconnectDelay = banDisconnectDelay

// kickNotice is the payload of the kicked event.
type kickNotice struct {
	Reason string `json:"reason"`
}

// RequirePermission is RequirePerm for code that is not a route (a slash
// command): the caller must hold every permission of need server-wide, the
// Owner always, and without a permission service only the Owner passes.
func (h *Handler) RequirePermission(r *http.Request, need perm.Set) error {
	u := UserFrom(r.Context())
	if u == nil {
		return errNoSession
	}
	if h.Perms == nil {
		if !u.IsAdmin() {
			return httpx.ErrForbidden("only the Owner can do this")
		}
		return nil
	}
	have, err := h.Perms.Global(r.Context(), u.Subject())
	if err != nil {
		return err
	}
	if name := have.MissingName(need); name != "" {
		return httpx.ErrForbidden("missing permission: " + name)
	}
	return nil
}

// KickMember sends a member away for now: every session of theirs ends (push
// devices and native clients included), their live connections close and they
// leave the Talk they are in. It is no sanction: nothing is recorded against
// the account and they can sign in again at once. The caller has checked the
// Kick members permission (RequirePermission); the role hierarchy is enforced
// like for a ban, and nobody can kick the Owner or themselves.
func (h *Handler) KickMember(r *http.Request, id uuid.UUID, reason string) error {
	reason, err := cleanReason(reason)
	if err != nil {
		return err
	}
	err = h.moderationTarget(r, id, func(q db.Querier) error {
		if err := RevokeSessions(r.Context(), q, id); err != nil {
			return err
		}
		_, err := q.Exec(r.Context(), `DELETE FROM push_subscriptions WHERE user_id = $1`, id)
		return err
	})
	if err != nil {
		return err
	}
	// Tell them first; their sessions are dead already, so nothing can be done
	// with the socket meanwhile.
	if h.Events != nil {
		h.Events.SendToUsers([]uuid.UUID{id}, events.Kicked, kickNotice{Reason: reason})
	}
	if h.Live != nil {
		h.Live.KickFromVoice(id)
		for _, ch := range h.Live.VoiceChannels(id) {
			h.Live.KickFromChannel(id, ch)
		}
		time.AfterFunc(kickDisconnectDelay, func() { h.Live.DisconnectUser(id) })
	}
	return nil
}
