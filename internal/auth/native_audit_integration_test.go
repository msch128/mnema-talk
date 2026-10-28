//go:build integration

package auth

import (
	"context"
	"net/http"
	"strconv"
	"testing"

	"github.com/msch128/mnema-talk/internal/audit"
)

func auditCount(t *testing.T, h *NativeHandler, action string) int {
	t.Helper()
	var n int
	if err := h.accounts.Sessions.DB.QueryRow(context.Background(), `SELECT COUNT(*) FROM audit_log WHERE action = $1`, action).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// A credential or account change through the native routes leaves the same
// audit entry the browser routes write.
func TestNativeRegistrationAndPasswordChangeAreAudited(t *testing.T) {
	pool, owner, h := nativeHTTPFixture(t)
	h.accounts.Audit = &audit.Log{DB: pool}
	h.accounts.Live = &nativePasswordObserver{}
	ctx := context.Background()
	if _, err := CreateInvite(ctx, pool, owner.ID, "audited-invite-code", nil, nil); err != nil {
		t.Fatal(err)
	}
	route, err := h.RegistrationHandler()
	if err != nil {
		t.Fatal(err)
	}
	nativeStatus(t, route, nativeRequest(http.MethodPost, "/auth/register", nativeRegisterBody("audited", "audited-invite-code")), http.StatusCreated)
	if got := auditCount(t, h, "auth.register"); got != 1 {
		t.Fatalf("auth.register entries = %d", got)
	}

	grant := nativeHTTPLogin(t, h, owner)
	change, err := h.PasswordChangeHandler()
	if err != nil {
		t.Fatal(err)
	}
	body := `{"current_password":"` + testPassword + `","new_password":"` + nativeNextPassword + `"}`
	nativeStatus(t, change, nativeAuthedRequest(http.MethodPut, "/auth/password", body, grant.Access), http.StatusOK)
	if got := auditCount(t, h, "auth.password_change"); got != 1 {
		t.Fatalf("auth.password_change entries = %d", got)
	}
}

// A saturated hashing queue tells the native client when to come back.
func TestNativeRegistrationBusyAnswersRetryAfter(t *testing.T) {
	pool, owner, h := nativeHTTPFixture(t)
	if _, err := CreateInvite(context.Background(), pool, owner.ID, "busy-invite-code", nil, nil); err != nil {
		t.Fatal(err)
	}
	route, err := h.RegistrationHandler()
	if err != nil {
		t.Fatal(err)
	}
	for range cap(bcryptSlots) {
		bcryptSlots <- struct{}{}
	}
	bcryptWaiting.Store(bcryptMaxQueue)
	t.Cleanup(func() {
		bcryptWaiting.Store(0)
		for range cap(bcryptSlots) {
			<-bcryptSlots
		}
	})
	w := nativeStatus(t, route, nativeRequest(http.MethodPost, "/auth/register", nativeRegisterBody("busy", "busy-invite-code")), http.StatusServiceUnavailable)
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("busy native registration carries no Retry-After")
	}
}

// An early refresh is told the time that is really left, not the full interval.
func TestNativeRefreshTooEarlyReportsRemainingWait(t *testing.T) {
	pool, user, h := nativeHTTPFixture(t)
	grant := nativeHTTPLogin(t, h, user)
	if _, err := pool.Exec(context.Background(), `UPDATE native_session_families SET refresh_after = NOW() + INTERVAL '10 seconds'`); err != nil {
		t.Fatal(err)
	}
	w := nativeStatus(t, h.Handler(), nativeRequest(http.MethodPost, "/auth/refresh", nativeRefreshBody(grant.Refresh)), http.StatusTooManyRequests)
	retry, err := strconv.Atoi(w.Header().Get("Retry-After"))
	if err != nil || retry < 1 || retry > 11 {
		t.Fatalf("Retry-After = %q, want about 10", w.Header().Get("Retry-After"))
	}
}
