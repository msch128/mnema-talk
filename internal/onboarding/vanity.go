package onboarding

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// The vanity invite link is one custom slug for the community, served at
// /join/<slug> (the Owner and Administrators set it, audited). By default it
// only takes a visitor to the normal sign-in page under a friendly "you were
// invited" header: registration stays invite-only and the link bypasses
// nothing. With accepts_signups the Owner turns it into a permanent public
// invite (unlimited uses, no expiry); the sign-up then names the slug as its
// code, "~<slug>" (a "~" cannot be part of a real invite code), and goes
// through the same Register as every invite.

const (
	// MinVanitySlug and MaxVanitySlug bound the slug length.
	MinVanitySlug = 3
	MaxVanitySlug = 32
)

// vanityPattern: lowercase letters and digits, single hyphens between them.
var vanityPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// reservedSlugs are words that name something of the app: its routes (the
// API, the web app's top-level paths), its static files and the words people
// expect to mean the service itself. A test keeps the list in step with the
// routes of web/src/lib/router.ts and the files in web/public.
var reservedSlugs = map[string]bool{
	"admin": true, "api": true, "assets": true, "c": true, "dm": true, "docs": true,
	"events": true, "favicon": true, "health": true, "help": true, "invite": true,
	"invites": true, "join": true, "legal": true, "login": true, "logout": true,
	"manifest": true, "metrics": true, "mnema": true, "mnema-talk": true, "privacy": true,
	"pwa": true, "register": true, "root": true, "security": true, "settings": true,
	"signup": true, "static": true, "status": true, "support": true, "sw": true,
	"terms": true, "v": true, "wiki": true, "ws": true, "www": true, "apple-touch-icon": true,
	"pwa-icon": true, "pwa-badge": true, "robots": true, "sitemap": true, "well-known": true,
}

// VanitySettings is the vanity link as the admin console shows it.
type VanitySettings struct {
	// Slug is empty while there is no vanity link.
	Slug string `json:"slug" maxLength:"32"`
	// AcceptsSignups: the link works as a permanent public invite.
	AcceptsSignups bool `json:"accepts_signups"`
	// Path is where the link lives on this server, empty without a slug; the
	// web app puts its address in front.
	Path string `json:"path" binding:"optional"`
}

// VanityInput is the body of PUT /api/admin/vanity-link; it replaces the setting.
type VanityInput struct {
	// Slug is 3 to 32 characters: lowercase letters, digits and single
	// hyphens inside. Upper case is folded to lower. Empty removes the link.
	Slug           string `json:"slug" maxLength:"32"`
	AcceptsSignups bool   `json:"accepts_signups"`
}

// CleanVanitySlug folds the slug to lower case and checks it.
func CleanVanitySlug(raw string) (string, error) {
	slug := strings.ToLower(strings.TrimSpace(raw))
	if slug == "" {
		return "", nil
	}
	if n := len(slug); n < MinVanitySlug || n > MaxVanitySlug {
		return "", httpx.ErrInvalidInput(fmt.Sprintf("the link name must be %d to %d characters", MinVanitySlug, MaxVanitySlug))
	}
	if !vanityPattern.MatchString(slug) {
		return "", httpx.ErrInvalidInput("the link name may only hold lowercase letters, digits and single hyphens between them")
	}
	if reservedSlugs[slug] {
		return "", httpx.ErrInvalidInput("this link name is reserved")
	}
	return slug, nil
}

func vanityOf(slug *string, accepts bool) VanitySettings {
	v := VanitySettings{AcceptsSignups: accepts && slug != nil}
	if slug != nil {
		v.Slug, v.Path = *slug, "/join/"+*slug
	}
	return v
}

func readVanity(q db.Querier, r *http.Request) (VanitySettings, error) {
	var slug *string
	var accepts bool
	if err := q.QueryRow(r.Context(), `SELECT vanity_slug, vanity_accepts_signups FROM instance_settings`).Scan(&slug, &accepts); err != nil {
		return VanitySettings{}, fmt.Errorf("read vanity link: %w", err)
	}
	return vanityOf(slug, accepts), nil
}

// getVanity handles GET /api/admin/vanity-link.
//
// @Summary Read the vanity invite link
// @Description Owner and Administrators only.
// @ID getVanityLink
// @Tags Onboarding
// @Produce json
// @Security cookieAuth
// @Success 200 {object} VanitySettings "Setting."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: only the Owner and members with the Administrator permission may read it."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/vanity-link [get]
func (h *Handler) getVanity(w http.ResponseWriter, r *http.Request) error {
	v, err := readVanity(h.DB, r)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, v)
	return nil
}

// putVanity handles PUT /api/admin/vanity-link.
//
// @Summary Set or remove the vanity invite link
// @Description Owner and Administrators only; audited. `slug` is 3 to 32 characters (lowercase letters, digits, single hyphens inside; upper case is folded) and must not be a reserved word (400 INVALID_INPUT); an empty slug removes the link. The link is served at /join/{slug}. By default it only leads visitors to the sign-in page: sign-up stays invite-only. With `accepts_signups` it works as a permanent public invite (unlimited uses, no expiry), so anyone who knows it can register; leave it off to keep invites individual. One link per community; setting it replaces the previous one.
// @ID setVanityLink
// @Tags Onboarding
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param body body VanityInput true "New setting."
// @Success 200 {object} VanitySettings "Setting."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, or a slug of the wrong length or characters, or reserved."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: only the Owner and members with the Administrator permission may change it, or cross-origin request rejected."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/vanity-link [put]
func (h *Handler) putVanity(w http.ResponseWriter, r *http.Request) error {
	var in VanityInput
	if err := httpx.DecodeJSON(r, &in); err != nil {
		return err
	}
	slug, err := CleanVanitySlug(in.Slug)
	if err != nil {
		return err
	}
	var value *string
	if slug != "" {
		value = &slug
	}
	// A link that does not exist cannot accept sign-ups.
	_, err = h.DB.Exec(r.Context(), `UPDATE instance_settings SET vanity_slug = $1, vanity_accepts_signups = $2, updated_at = NOW()`,
		value, in.AcceptsSignups && value != nil)
	if err != nil {
		return fmt.Errorf("write vanity link: %w", err)
	}
	v, err := readVanity(h.DB, r)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, v)
	return nil
}
