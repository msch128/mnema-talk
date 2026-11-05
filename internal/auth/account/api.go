// Package account is the member's own data: the GDPR export, the erasure of
// an account and the routes for them. It is split from package auth, which
// keeps the authentication core, so that the code that removes and copies
// personal data is reviewed apart from the code that signs people in.
package account

import (
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/userdata"
)

// API serves the account export and deletion routes. It embeds the auth
// Handler on purpose: the server fills Handler.Objects, Mail, Lifetime and the
// other collaborators after construction, and the embedded pointer always sees
// the current values.
type API struct {
	*auth.Handler
	// personal is the other domains' export files and erasure statements.
	personal *userdata.Registry
	// exportSlots bounds the exports built at once.
	exportSlots chan struct{}
	// downloadPerUser bounds export downloads.
	downloadPerUser *httpx.RateLimiter
}

// New returns the account routes of h.
func New(h *auth.Handler) *API {
	return &API{
		Handler:         h,
		exportSlots:     make(chan struct{}, exportSlots),
		downloadPerUser: httpx.NewRateLimiter(10, time.Hour).Named("export_download"),
	}
}

// MountAuthenticated registers the signed-in export and deletion routes.
func (h *API) MountAuthenticated(r chi.Router) {
	r.Post("/users/me/export", httpx.Handle(h.requestExport))
	r.Get("/users/me/export", httpx.Handle(h.exportStatus))
	r.With(h.downloadPerUser.By(auth.UserKey)).Get("/users/me/export/download", httpx.Handle(h.downloadExport))
	r.Post("/users/me/delete", httpx.Handle(h.deleteAccount))
}
