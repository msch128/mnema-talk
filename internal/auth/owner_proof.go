package auth

import "net/http"

// OwnerProof is the Owner's confirmation of their own identity for taking over
// another account. Both fields are optional in the schema so existing clients
// keep validating, but the server refuses the request without them.
type OwnerProof struct {
	// CurrentPassword is the Owner's own password.
	CurrentPassword string `json:"current_password,omitempty" format:"password" binding:"optional"`
	// Code is a current authenticator code or an unused recovery code; needed
	// only while the Owner has two-factor sign-in on.
	Code string `json:"code,omitempty" maxLength:"32" binding:"optional"`
}

// confirmOwner re-authenticates the signed-in Owner before a takeover action
// with the same check, lockouts and errors as deleting an account. It reports
// false when the response was already written.
func (h *Handler) confirmOwner(w http.ResponseWriter, r *http.Request, proof OwnerProof) (bool, error) {
	return h.ConfirmIdentity(w, r, UserFrom(r.Context()), proof.CurrentPassword, proof.Code)
}
