package onboarding

import (
	"errors"
	"net/http"
	"regexp"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// InvitePreview is the public answer about an invite code. It carries nothing
// beyond whether the code works, the community's name and when it expires: no
// creator, no use counts. An expired, used-up, revoked or never-issued code
// all answer the same {"valid": false}.
type InvitePreview struct {
	Valid         bool       `json:"valid"`
	CommunityName string     `json:"community_name,omitempty" binding:"optional"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty" binding:"optional" format:"date-time"`
}

// previewCodePattern matches the codes the server issues and accepts (see
// auth.CreateInvite); anything else is answered without a database lookup.
var previewCodePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{6,64}$`)

// previewInvite handles GET /api/invites/{code}.
//
// @Summary Preview an invite
// @Description Public, no session. Says whether an invite code can still be used and, if so, the community's name and when the code expires. Unknown, expired, revoked and used-up codes all answer the same `{"valid": false}`; nothing else about the invite is revealed. Rate limited per IP (30 per minute, IPv6 per /64).
// @ID previewInvite
// @Tags Auth
// @Produce json
// @Param code path string true "Invite code."
// @Success 200 {object} InvitePreview "Preview."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/invites/{code} [get]
func (h *Handler) previewInvite(w http.ResponseWriter, r *http.Request) error {
	w.Header().Set("Cache-Control", "no-store")
	code := chi.URLParam(r, "code")
	if !previewCodePattern.MatchString(code) {
		httpx.WriteJSON(w, http.StatusOK, InvitePreview{})
		return nil
	}
	var out InvitePreview
	var expires *time.Time
	err := h.DB.QueryRow(r.Context(), `
		SELECT s.community_name, i.expires_at
		FROM invites i CROSS JOIN instance_settings s
		WHERE i.code = $1
		  AND (i.expires_at IS NULL OR i.expires_at > NOW())
		  AND (i.max_uses IS NULL OR i.uses_count < i.max_uses)`, code).Scan(&out.CommunityName, &expires)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteJSON(w, http.StatusOK, InvitePreview{})
		return nil
	}
	if err != nil {
		return err
	}
	out.Valid, out.ExpiresAt = true, expires
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}
