// Package onboarding serves the first-run side of the community: its name,
// welcome text and rules, who has accepted them, a member's dismissed tips and
// the public invite preview.
//
// Rules are enforced by package perm (a member who has not accepted the
// current rules loses what a timeout takes: posting, reacting, uploading,
// joining a Talk), so REST, the WebSocket hub and the SFU all agree. An empty
// rules text enforces nothing, which is what keeps upgrades from locking
// anyone out. The Owner and bots are exempt.
package onboarding

import (
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// Handler serves /api/invites/{code}, /api/onboarding and /api/admin/onboarding.
type Handler struct {
	DB    *db.Pool
	Perms *perm.Service

	previewPerIP *httpx.RateLimiter
	// lookupMisses is the global budget of failed code lookups, whatever the
	// address: rotating IPv6 prefixes or a botnet gets no further than this.
	lookupMisses *httpx.RateLimiter
	// missPerIP is the primary limit on unknown codes per client address.
	missPerIP *httpx.RateLimiter
}

// maxInviteLookupMisses is how many unknown or unusable codes the public
// preview answers per hour across all clients before unknown codes get 429.
const maxInviteLookupMisses = 600

// NewHandler builds the handler with the invite preview's per-IP budget.
func NewHandler(pool *db.Pool, perms *perm.Service) *Handler {
	return &Handler{
		DB:    pool,
		Perms: perms,
		// A preview costs one indexed lookup; the budget only has to make
		// guessing codes pointless (a code carries about 59 bits).
		previewPerIP: httpx.NewRateLimiter(30, time.Minute).Named("invite_preview_ip"),
		lookupMisses: httpx.NewRateLimiter(maxInviteLookupMisses, time.Hour),
		missPerIP:    httpx.NewRateLimiter(20, time.Hour),
	}
}

// MountPublic registers the route that needs no session.
func (h *Handler) MountPublic(r chi.Router) {
	r.With(h.previewPerIP.PerIP).Get("/invites/{code}", httpx.Handle(h.previewInvite))
}

// Mount registers the routes every signed-in member may call.
func (h *Handler) Mount(r chi.Router) {
	r.Get("/onboarding", httpx.Handle(h.getState))
	r.Post("/onboarding/accept", httpx.Handle(h.accept))
	r.Put("/onboarding/tips", httpx.Handle(h.putTips))
}

// MountAdmin registers the settings routes; the Owner and Administrators only
// (RequireUser applies).
func (h *Handler) MountAdmin(r chi.Router) {
	admin := r.With(auth.RequirePerm(h.Perms, perm.Administrator))
	admin.Get("/onboarding", httpx.Handle(h.getSettings))
	admin.Put("/onboarding", httpx.Handle(h.putSettings))
}
