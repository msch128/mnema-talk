package memberadmin

import (
	"net/http"

	"github.com/msch128/mnema-talk/internal/auth"
)

// withRoles fills RoleIDs of the given users from the permission state.
func (h *API) withRoles(r *http.Request, users []auth.AdminUser) error {
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
