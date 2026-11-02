package onboarding

import (
	"context"
	"errors"
	"net/http"
	"regexp"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// JoinPreview is the public answer about a vanity link. Like the invite
// preview it carries nothing beyond whether the link exists, the community's
// name and whether the link takes sign-ups; an unknown slug answers
// {"valid": false}.
type JoinPreview struct {
	Valid         bool   `json:"valid"`
	CommunityName string `json:"community_name,omitempty" binding:"optional"`
	// AcceptsSignups: the link is a permanent public invite, and the sign-up
	// names the slug as "~<slug>" in invite_code.
	AcceptsSignups bool `json:"accepts_signups,omitempty" binding:"optional"`
}

// joinSlugPattern is what a request may name before the database is asked;
// it is wider than the slugs the Owner can set (any case), never narrower.
var joinSlugPattern = regexp.MustCompile(`^[A-Za-z0-9-]{3,32}$`)

// previewJoin handles GET /api/join/{slug}.
//
// @Summary Preview a vanity invite link
// @Description Public, no session. Says whether the vanity link /join/{slug} exists and, if so, the community's name and whether it accepts sign-ups. When it does, register with invite_code "~{slug}"; when it does not, the visitor signs in or needs an individual invite code. Unknown slugs answer `{"valid": false}`. Rate limited per IP like the invite preview (30 per minute, IPv6 per /64).
// @ID previewJoinLink
// @Tags Auth
// @Produce json
// @Param slug path string true "Link name."
// @Success 200 {object} JoinPreview "Preview."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/join/{slug} [get]
func (h *Handler) previewJoin(w http.ResponseWriter, r *http.Request) error {
	w.Header().Set("Cache-Control", "no-store")
	p, err := h.lookupJoin(r.Context(), chi.URLParam(r, "slug"))
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, p)
	return nil
}

// lookupJoin reads the vanity link a slug names; an unknown or malformed slug
// is the empty (invalid) preview.
func (h *Handler) lookupJoin(ctx context.Context, slug string) (JoinPreview, error) {
	if !joinSlugPattern.MatchString(slug) {
		return JoinPreview{}, nil
	}
	var p JoinPreview
	err := h.DB.QueryRow(ctx, `
		SELECT community_name, vanity_accepts_signups FROM instance_settings
		WHERE vanity_slug = LOWER($1)`, slug).Scan(&p.CommunityName, &p.AcceptsSignups)
	if errors.Is(err, pgx.ErrNoRows) {
		return JoinPreview{}, nil
	}
	if err != nil {
		return JoinPreview{}, err
	}
	p.Valid = true
	return p, nil
}

// JoinShareInfo is what the link preview of /join/{slug} shows: the name of
// the community behind a vanity link. ok is false for an unknown slug and for
// a caller over the invite preview budget (both get the generic page).
func (h *Handler) JoinShareInfo(r *http.Request, slug string) (info ShareInfo, ok bool, err error) {
	if !joinSlugPattern.MatchString(slug) {
		return ShareInfo{}, false, nil
	}
	if allowed, _ := h.previewPerIP.Allow(httpx.ClientIPKey(r)); !allowed {
		return ShareInfo{}, false, nil
	}
	if allowed, _ := h.previewPerSite.Allow(httpx.ClientAggregateKey(r)); !allowed {
		return ShareInfo{}, false, nil
	}
	p, err := h.lookupJoin(r.Context(), slug)
	if err != nil || !p.Valid {
		return ShareInfo{}, false, err
	}
	return ShareInfo{CommunityName: p.CommunityName}, true, nil
}
