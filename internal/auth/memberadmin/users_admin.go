package memberadmin

import (
	"github.com/msch128/mnema-talk/internal/auth"

	"github.com/go-chi/chi/v5"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

func (h *API) mountUserAdmin(r chi.Router) {
	r.With(auth.RequireAnyPerm(h.Perms, perm.ManageMembers|perm.ManageRoles)).Get("/users", httpx.Handle(h.listUsers))
	members := r.With(auth.RequirePerm(h.Perms, perm.ManageMembers))
	members.Post("/users/{id}/disable", httpx.Handle(h.disableUser))
	members.Post("/users/{id}/enable", httpx.Handle(h.enableUser))
	members.Post("/users/{id}/sessions/revoke", httpx.Handle(h.revokeUserSessions))
	members.Put("/users/{id}/status", httpx.Handle(h.setUserStatus))
	// Taking over an account is not a role's business: the Owner only.
	r.With(auth.RequireOwner).Post("/users/{id}/password", httpx.Handle(h.setUserPassword))
	r.With(auth.RequireOwner).Post("/users/{id}/password-reset", httpx.Handle(h.resetUserPassword))
	r.With(auth.RequireOwner).Post("/users/{id}/login-lockout/clear", httpx.Handle(h.ClearUserLockout))
	r.With(auth.RequireOwner).Post("/users/{id}/two-factor/reset", httpx.Handle(h.resetUserTwoFactor))
	r.With(auth.RequirePerm(h.Perms, perm.DisconnectMembers)).Post("/users/{id}/kick", httpx.Handle(h.kickUser))
	h.mountModeration(r)
}
