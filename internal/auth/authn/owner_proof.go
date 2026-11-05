package authn

import (
	"net/http"

	"github.com/msch128/mnema-talk/internal/auth"
)

// ConfirmOwner re-authenticates the signed-in Owner before a takeover action
// with the same check, lockouts and errors as deleting an account. It reports
// false when the response was already written.
func (h *Handler) ConfirmOwner(w http.ResponseWriter, r *http.Request, proof auth.OwnerProof) (bool, error) {
	return h.ConfirmIdentity(w, r, auth.UserFrom(r.Context()), proof.CurrentPassword, proof.Code)
}
