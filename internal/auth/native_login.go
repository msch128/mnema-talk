package auth

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/httpx"
)

func nativeLoginFields(fields map[string]string) (uuid.UUID, error) {
	if len(fields["username"]) > 64 || len(fields["password"]) > MaxPasswordBytes {
		return uuid.Nil, httpx.ErrInvalidInput("invalid native credential fields")
	}
	instanceID, err := uuid.Parse(fields["client_instance_id"])
	if err != nil || instanceID == uuid.Nil {
		return uuid.Nil, httpx.ErrInvalidInput("invalid native client instance")
	}
	if instanceID.String() != fields["client_instance_id"] {
		return uuid.Nil, httpx.ErrInvalidInput("invalid native client instance")
	}
	return instanceID, nil
}

func (h *NativeHandler) login(w http.ResponseWriter, r *http.Request) error {
	if err := noNativeAuthorization(r); err != nil {
		return err
	}
	fields, err := decodeNativeStrings(w, r, "username", "password", "client_instance_id")
	if err != nil {
		return nativeHandlerError(err)
	}
	instanceID, err := nativeLoginFields(fields)
	if err != nil {
		return err
	}
	user, version, retry, err := h.accounts.verifyLogin(r, LoginRequest{Username: fields["username"], Password: fields["password"]})
	if retry > 0 {
		httpx.WriteRateLimited(w, retry)
		return nil
	}
	if err != nil {
		return nativeHandlerError(err)
	}
	if challenged, err := h.accounts.challengeIfNeeded(r.Context(), w, user, version); err != nil {
		return nativeHandlerError(err)
	} else if challenged {
		return nil
	}
	h.accounts.auditUser(r.Context(), user, "auth.login")
	grant, err := h.sessions.IssueVerified(r.Context(), verifiedNativeLogin{userID: user.ID, tokenVersion: version}, instanceID)
	if err != nil {
		return nativeHandlerError(err)
	}
	writeNativeGrant(w, grant)
	return nil
}

// login2fa finishes a native sign-in of an account with two-factor sign-in.
func (h *NativeHandler) login2fa(w http.ResponseWriter, r *http.Request) error {
	if err := noNativeAuthorization(r); err != nil {
		return err
	}
	fields, err := decodeNativeStrings(w, r, "challenge", "code", "client_instance_id")
	if err != nil {
		return nativeHandlerError(err)
	}
	if len(fields["code"]) > 32 {
		return httpx.ErrInvalidInput("invalid native credential fields")
	}
	instanceID, err := uuid.Parse(fields["client_instance_id"])
	if err != nil || instanceID == uuid.Nil || instanceID.String() != fields["client_instance_id"] {
		return httpx.ErrInvalidInput("invalid native client instance")
	}
	user, version, retry, err := h.accounts.completeSecondFactor(r.Context(), httpx.ClientIPKey(r), fields["challenge"], fields["code"])
	if retry > 0 {
		httpx.WriteRateLimited(w, retry)
		return nil
	}
	if err != nil {
		return nativeHandlerError(err)
	}
	grant, err := h.sessions.IssueVerified(r.Context(), verifiedNativeLogin{userID: user.ID, tokenVersion: version}, instanceID)
	if err != nil {
		return nativeHandlerError(err)
	}
	writeNativeGrant(w, grant)
	return nil
}
