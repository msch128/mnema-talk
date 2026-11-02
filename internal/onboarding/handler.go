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
	"github.com/msch128/mnema-talk/internal/community"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/media"
	"github.com/msch128/mnema-talk/internal/perm"
)

// Handler serves /api/invites/{code}, /api/onboarding and /api/admin/onboarding.
type Handler struct {
	DB    *db.Pool
	Perms *perm.Service
	// Names caches the community name; it is dropped when the Owner saves the
	// settings. Events then tells open clients. Both are optional.
	Names  *community.Names
	Events events.Publisher
	// Store holds the community icon; without it the icon cannot be set and
	// reads as not set.
	Store media.Store

	iconPerIP    *httpx.RateLimiter
	previewPerIP *httpx.RateLimiter
	// previewPerSite caps a whole IPv6 /48 (or one IPv4 address) together, so
	// rotating /64 prefixes inside one allocation gains nothing.
	previewPerSite *httpx.RateLimiter
}

// NewHandler builds the handler with the invite preview's per-IP budget.
func NewHandler(pool *db.Pool, perms *perm.Service) *Handler {
	return &Handler{
		DB:    pool,
		Perms: perms,
		// The icon is a few KB read from storage; this only stops a loop.
		iconPerIP: httpx.NewRateLimiter(300, time.Minute).Named("community_icon_ip"),
		// A preview costs one indexed lookup; the budget only has to make
		// guessing codes pointless (a code carries about 59 bits).
		previewPerIP:   httpx.NewRateLimiter(30, time.Minute).Named("invite_preview_ip"),
		previewPerSite: httpx.NewRateLimiter(120, time.Minute).Named("invite_preview_site"),
	}
}

// MountPublic registers the route that needs no session.
func (h *Handler) MountPublic(r chi.Router) {
	h.mountPublicIcon(r)
	r.With(h.previewPerSite.By(httpx.ClientAggregateKey), h.previewPerIP.PerIP).Get("/join/{slug}", httpx.Handle(h.previewJoin))
	r.With(h.previewPerSite.By(httpx.ClientAggregateKey), h.previewPerIP.PerIP).Get("/invites/{code}", httpx.Handle(h.previewInvite))
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
	admin.Get("/vanity-link", httpx.Handle(h.getVanity))
	admin.Put("/vanity-link", httpx.Handle(h.putVanity))
	owner := r.With(auth.RequireOwner)
	owner.Get("/first-run", httpx.Handle(h.getFirstRun))
	owner.Put("/first-run", httpx.Handle(h.putFirstRun))
}
