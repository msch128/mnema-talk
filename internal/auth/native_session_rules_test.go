package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestParseNativeInstanceIsCanonicalAndNonNil(t *testing.T) {
	id := uuid.New()
	if got, err := parseNativeInstance(id.String()); err != nil || got != id {
		t.Fatalf("canonical id: %v %v", got, err)
	}
	for _, raw := range []string{"", "nope", uuid.Nil.String(), strings.ToUpper(id.String()), "{" + id.String() + "}", strings.ReplaceAll(id.String(), "-", "")} {
		if _, err := parseNativeInstance(raw); err == nil {
			t.Errorf("%q accepted", raw)
		}
	}
}

func TestNativeLoginBusyAnswersRetryAfter(t *testing.T) {
	h := nativeUnitHandler(t)
	h.accounts.verify = func(context.Context, string, string) (*User, int, error) { return nil, 0, ErrServerBusy }
	body := nativeLoginBodyUnit("herzog", testPassword, uuid.New())
	w := nativeStatus(t, h.Handler(), nativeRequest(http.MethodPost, "/auth/login", body), http.StatusServiceUnavailable)
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("busy native login carries no Retry-After")
	}
}

func nativeLoginBodyUnit(username, password string, instance uuid.UUID) string {
	return `{"username":"` + username + `","password":"` + password + `","client_instance_id":"` + instance.String() + `"}`
}

// One predicate decides whether a locked native session may still be used; a
// new condition cannot be honoured by lease and reissue and forgotten by
// logout or refresh.
func TestLockedNativeSessionPredicatesAgree(t *testing.T) {
	now := time.Now()
	instance := uuid.New()
	principal := NativePrincipal{instanceID: instance, tokenVersion: 3,
		accessExpiresAt: now.Add(time.Minute), familyExpiresAt: now.Add(time.Hour)}
	live := func() lockedNativeSession {
		return lockedNativeSession{version: 3, family: nativeFamily{instanceID: instance, version: 3, expiresAt: now.Add(time.Hour)}}
	}
	revoked := now.Add(-time.Second)
	for name, tc := range map[string]struct {
		mutate     func(*lockedNativeSession)
		lease      bool // rejected by lease and reissue
		logout     bool
		refreshEnd bool
	}{
		"live":           {func(*lockedNativeSession) {}, false, false, false},
		"disabled":       {func(l *lockedNativeSession) { l.disabled = true }, true, true, true},
		"account moved":  {func(l *lockedNativeSession) { l.version = 4; l.family.version = 4 }, true, true, false},
		"family moved":   {func(l *lockedNativeSession) { l.family.version = 2 }, true, true, false},
		"other instance": {func(l *lockedNativeSession) { l.family.instanceID = uuid.New() }, true, true, false},
		"family expired": {func(l *lockedNativeSession) { l.family.expiresAt = now }, true, true, true},
		// Logging out twice succeeds, so a revoked family is no logout error.
		"revoked": {func(l *lockedNativeSession) { l.family.revokedAt = &revoked }, true, false, true},
	} {
		t.Run(name, func(t *testing.T) {
			l := live()
			tc.mutate(&l)
			if got := l.leaseRejected(now, principal); got != tc.lease {
				t.Errorf("lease rejected = %v", got)
			}
			expected := principal
			expected.familyExpiresAt = l.family.expiresAt
			if got := l.reissueLeaseRejected(now, expected); got != tc.lease {
				t.Errorf("reissue rejected = %v", got)
			}
			if got := l.logoutRejected(now, principal); got != tc.logout {
				t.Errorf("logout rejected = %v", got)
			}
			if got := l.closed(now); got != tc.refreshEnd {
				t.Errorf("refresh closed = %v", got)
			}
		})
	}
}

func TestNativeRefreshRetryReportsTheRemainingWait(t *testing.T) {
	wait := refreshWaitError{retry: 10 * time.Second}
	if !errors.Is(wait, ErrNativeRefreshWait) {
		t.Fatal("typed wait is not ErrNativeRefreshWait")
	}
	if got := nativeRefreshRetry(wait); got != 10*time.Second {
		t.Fatalf("retry = %v", got)
	}
	if got := nativeRefreshRetry(ErrNativeRefreshWait); got != nativeRefreshInterval {
		t.Fatalf("bare sentinel retry = %v", got)
	}
}
