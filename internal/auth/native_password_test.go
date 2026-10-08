package auth

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
)

type nativePasswordObserver struct{ calls int }

func (o *nativePasswordObserver) DisconnectNativeCredentials(uuid.UUID) { o.calls++ }
func (o *nativePasswordObserver) DisconnectUser(uuid.UUID)              {}
func (o *nativePasswordObserver) KickFromVoice(uuid.UUID) bool          { return false }
func (o *nativePasswordObserver) SetPresence(uuid.UUID, string)         {}

func TestNativePasswordDependenciesAndBoundary(t *testing.T) {
	for _, h := range []*NativeHandler{nil, {}, {accounts: &Handler{}}} {
		if _, err := h.PasswordChangeHandler(); err == nil {
			t.Fatal("invalid dependencies accepted")
		}
	}
	h := nativeUnitHandler(t)
	if _, err := h.PasswordChangeHandler(); err == nil {
		t.Fatal("missing bulk credential cutoff accepted")
	}
	h.accounts.Live = &nativePasswordObserver{}
	router, err := h.PasswordChangeHandler()
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"Origin", "Cookie", "oRiGiN", "cOoKiE"} {
		r := nativeRequest(http.MethodPut, "/auth/password", "{}")
		r.Header[key] = []string{""}
		nativeStatus(t, router, r, http.StatusForbidden)
	}
	for _, path := range []string{"/auth/password?", "/auth/password?a=b", "/auth/%70assword"} {
		nativeStatus(t, router, nativeRequest(http.MethodPut, path, "{}"), http.StatusBadRequest)
	}
	nativeStatus(t, router, nativeRequest(http.MethodGet, "/auth/password", ""), http.StatusMethodNotAllowed)
	nativeStatus(t, router, nativeRequest(http.MethodPut, "/missing", "{}"), http.StatusNotFound)
	nativeStatus(t, router, nativeRequest(http.MethodPut, "/auth/password", "{}"), http.StatusUnauthorized)
}

func TestNativePasswordRejectsUnqualifiedPrincipalBeforeDatabase(t *testing.T) {
	s := &NativeSessions{}
	control := &nativePasswordObserver{}
	if _, err := s.changePasswordAndReissue(context.Background(), NativePrincipal{}, "synthetic-current", "synthetic-next", control); !errors.Is(err, ErrNativeUnauthorized) {
		t.Fatal("missing authorization accepted")
	}
	p := NativePrincipal{user: User{ID: uuid.New()}, familyID: uuid.New(), instanceID: uuid.New()}
	if _, err := s.changePasswordAndReissue(context.Background(), p, "synthetic-current", "short", control); err == nil {
		t.Fatal("invalid password accepted")
	}
	if _, err := s.changePasswordAndReissue(context.Background(), p, "synthetic-current", "synthetic-next", nil); !errors.Is(err, ErrNativeUnauthorized) {
		t.Fatal("missing cutoff accepted")
	}
	if control.calls != 0 {
		t.Fatal("unqualified request retired transports")
	}
}
