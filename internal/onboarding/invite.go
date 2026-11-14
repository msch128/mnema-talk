package onboarding

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// InvitePreview is the public answer about an invite code. It carries nothing
// beyond whether the code works, the community's name, when it expires and the
// channel it lands in: no creator, no use counts. An expired, used-up, revoked or never-issued code
// all answer the same {"valid": false}.
type InvitePreview struct {
	Valid         bool       `json:"valid"`
	CommunityName string     `json:"community_name,omitempty" binding:"optional"`
	ExpiresAt     *time.Time `json:"expires_at,omitempty" binding:"optional" format:"date-time"`
	// ChannelID is the channel the invite lands the new member in. It is left
	// out for an invite without one, and for a channel that a plain new
	// member could not see: they land in the default channel then.
	ChannelID *uuid.UUID `json:"channel_id,omitempty" binding:"optional" format:"uuid"`
}

// previewCodePattern matches the codes the server issues and accepts (see
// auth.CreateInvite); anything else is answered without a database lookup.
var previewCodePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{6,64}$`)

// previewInvite handles GET /api/invites/{code}.
//
// @Summary Preview an invite
// @Description Public, no session. Says whether an invite code can still be used and, if so, the community's name, when the code expires and the channel it lands the new member in (only one every new member can see). Unknown, weak legacy custom, expired, revoked and used-up codes all answer the same `{"valid": false}`; nothing else about the invite is revealed. Rate limited per IP (30 per minute, IPv6 per /64)
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
	row, err := h.lookupInvite(r.Context(), code)
	if errors.Is(err, pgx.ErrNoRows) {
		httpx.WriteJSON(w, http.StatusOK, InvitePreview{})
		return nil
	}
	if err != nil {
		return err
	}
	landing, err := h.landingChannel(r.Context(), row.channelID)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, InvitePreview{Valid: true, CommunityName: row.communityName, ExpiresAt: row.expiresAt, ChannelID: landing})
	return nil
}

// inviteRow is what a usable invite code shows to someone who holds it.
type inviteRow struct {
	communityName string
	welcomeText   string
	expiresAt     *time.Time
	channelID     *uuid.UUID
}

// landingChannel is the invite's channel when every new member sees it
// through @everyone alone; otherwise nil, and the joiner lands in the default
// channel. Roles a member gets later can show them more, but the invite never
// points anyone at a channel they could not see.
func (h *Handler) landingChannel(ctx context.Context, ch *uuid.UUID) (*uuid.UUID, error) {
	if ch == nil {
		return nil, nil
	}
	snap, err := h.Perms.ForChannel(ctx, *ch)
	if err != nil {
		return nil, err
	}
	if !snap.EveryoneCanView(*ch) {
		return nil, nil
	}
	return ch, nil
}

// lookupInvite reads the community behind a code that can still be used. An
// unknown, expired, used-up or too weak legacy code, and one whose creator
// is disabled or banned, is pgx.ErrNoRows.
func (h *Handler) lookupInvite(ctx context.Context, code string) (inviteRow, error) {
	var out inviteRow
	err := h.DB.QueryRow(ctx, `
		SELECT s.community_name, s.welcome_text, i.expires_at, i.channel_id
		FROM invites i JOIN users c ON c.id = i.created_by CROSS JOIN instance_settings s
		WHERE i.code = $1
		  AND `+auth.ActiveSQL("c")+`
		  AND (i.expires_at IS NULL OR i.expires_at > NOW())
		  AND (i.max_uses IS NULL OR i.uses_count < i.max_uses)`, code).Scan(&out.communityName, &out.welcomeText, &out.expiresAt, &out.channelID)
	// A legacy custom code below today's floor is guessable, so it no longer
	// works: it answers exactly like an unknown code.
	if err == nil && auth.IsWeakInviteCode(code) {
		err = pgx.ErrNoRows
	}
	return out, err
}
