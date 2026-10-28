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
	return parseNativeInstance(fields["client_instance_id"])
}

// parseNativeInstance accepts only the canonical, non-nil text form of a
// client instance ID.
func parseNativeInstance(raw string) (uuid.UUID, error) {
	instanceID, err := uuid.Parse(raw)
	if err != nil || instanceID == uuid.Nil || instanceID.String() != raw {
		return uuid.Nil, httpx.ErrInvalidInput("invalid native client instance")
	}
	return instanceID, nil
}

// nativeFail maps err for the client and, when a saturated hashing queue is the
// cause, adds the Retry-After the browser routes send too.
func nativeFail(w http.ResponseWriter, err error) error {
	setRetryIfBusy(w, err)
	return nativeHandlerError(err)
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
		return nativeFail(w, err)
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
	instanceID, err := parseNativeInstance(fields["client_instance_id"])
	if err != nil {
		return err
	}
	user, version, retry, err := h.accounts.completeSecondFactor(r.Context(), httpx.ClientIPKey(r), fields["challenge"], fields["code"])
	if retry > 0 {
		httpx.WriteRateLimited(w, retry)
		return nil
	}
	if err != nil {
		return nativeFail(w, err)
	}
	grant, err := h.sessions.IssueVerified(r.Context(), verifiedNativeLogin{userID: user.ID, tokenVersion: version}, instanceID)
	if err != nil {
		return nativeHandlerError(err)
	}
	writeNativeGrant(w, grant)
	return nil
}
