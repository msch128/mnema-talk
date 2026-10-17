package auth

import (
	"net/http"

	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// Subject is u for permission questions: the Owner is the account with the
// admin role.
func (u User) Subject() perm.Subject { return perm.Subject{ID: u.ID, Owner: u.IsAdmin()} }

// RequireOwner admits only the Owner. It must run after RequireUser. Powers
// that no role can grant (the system and storage areas, other members'
// passwords) sit behind it.
func RequireOwner(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u := UserFrom(r.Context()); u == nil || !u.IsAdmin() {
			httpx.WriteError(w, httpx.ErrForbidden("only the Owner can do this"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequirePerm admits members who hold every permission in need server-wide
// (the Owner always). Without a permission service only the Owner passes.
func RequirePerm(svc *perm.Service, need perm.Set) func(http.Handler) http.Handler {
	return guard(svc, func(p perm.Set) error {
		if name := p.MissingName(need); name != "" {
			return httpx.ErrForbidden("missing permission: " + name)
		}
		return nil
	})
}

// RequireAnyPerm admits members who hold at least one permission of any.
func RequireAnyPerm(svc *perm.Service, any perm.Set) func(http.Handler) http.Handler {
	return guard(svc, func(p perm.Set) error {
		if !p.HasAny(any) {
			return httpx.ErrForbidden("missing permission")
		}
		return nil
	})
}

func guard(svc *perm.Service, ok func(perm.Set) error) func(http.Handler) http.Handler {
	if svc == nil {
		return func(next http.Handler) http.Handler { return RequireOwner(next) }
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u := UserFrom(r.Context())
			if u == nil {
				httpx.WriteError(w, errNoSession)
				return
			}
			p, err := svc.Global(r.Context(), u.Subject())
			if err == nil {
				err = ok(p)
			}
			if err != nil {
				if _, isAPI := httpx.AsAPIError(err); !isAPI {
					err = httpx.ErrServer(err)
				}
				httpx.WriteError(w, err)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// authorizeTarget checks that the caller may act on another member: never on
// the Owner, and only on members whose highest role is below the caller's.
func (h *Handler) authorizeTarget(r *http.Request, target *User) error {
	if target.IsAdmin() {
		return errOwnerAction
	}
	actor := UserFrom(r.Context())
	if h.Perms == nil || actor == nil || actor.IsAdmin() {
		return nil
	}
	snap, err := h.Perms.Snapshot(r.Context())
	if err != nil {
		return err
	}
	return snap.CanModerate(actor.Subject(), target.Subject())
}

// withRoles fills RoleIDs of the given users from the permission state.
func (h *Handler) withRoles(r *http.Request, users []AdminUser) error {
	if h.Perms == nil {
		return nil
	}
	roles, err := h.Perms.MemberRoles(r.Context())
	if err != nil {
		return err
	}
	for i := range users {
		users[i].RoleIDs = roles[users[i].ID]
	}
	return nil
}
