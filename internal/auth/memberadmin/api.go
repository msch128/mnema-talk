// Package memberadmin holds the admin routes that act on members: invites,
// bans and timeouts, kicks, account enabling and password resets, and the
// voice-admin disconnect. It is split from package auth, which keeps the
// authentication core (sessions, sign-in, two-factor, the User in the request
// context), so that code that decides who may sign in is reviewed apart from
// code that lets staff act on accounts.
package memberadmin

import "github.com/msch128/mnema-talk/internal/auth"

// API serves the member-admin routes. It embeds the auth Handler on purpose:
// the server fills Handler.Live, Perms, Events and the other collaborators
// after construction, and the embedded pointer always sees the current values,
// so the routes behave exactly as they did when they lived on the Handler.
type API struct {
	*auth.Handler
}

// New returns the member-admin routes of h.
func New(h *auth.Handler) *API { return &API{Handler: h} }
