//go:build integration

package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const nativeNextPassword = "synthetic-new-password"

type nativePasswordConcurrentObserver struct{ calls atomic.Int32 }

func (o *nativePasswordConcurrentObserver) DisconnectNativeCredentials(uuid.UUID) { o.calls.Add(1) }

type nativePasswordCanceledCommitTx struct{ pgx.Tx }

func (tx nativePasswordCanceledCommitTx) Commit(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	return tx.Tx.Commit(ctx)
}

type nativePasswordFaultQueryTx struct {
	pgx.Tx
	query string
}

func (tx nativePasswordFaultQueryTx) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	if query == tx.query {
		var cancel context.CancelFunc
		ctx, cancel = context.WithCancel(ctx)
		cancel()
	}
	return tx.Tx.QueryRow(ctx, query, args...)
}

func TestNativePasswordAtomicNewFamilyAndOldSessionInvalidation(t *testing.T) {
	pool, user, service, proof := nativeFixture(t)
	old := nativeIssue(t, service, proof)
	other := nativeIssue(t, service, proof)
	control := &nativePasswordObserver{}
	fresh, err := service.changePasswordAndReissue(context.Background(), old.Principal(), testPassword, nativeNextPassword, control)
	if err != nil {
		t.Fatal(err)
	}
	if control.calls != 1 || fresh.Principal().FamilyID() == old.Principal().FamilyID() || fresh.Principal().ClientInstanceID() != old.Principal().ClientInstanceID() || fresh.refreshSequence != 0 || fresh.Principal().TokenVersion() != proof.tokenVersion+1 || fresh.Principal().User().ID != user.ID || fresh.AccessExpiresAt().Sub(fresh.FamilyExpiresAt()) != nativeAccessTTL-nativeMaxFamilyTTL {
		t.Fatal("credential replacement is not a new bounded initial grant")
	}
	for _, grant := range []IssuedNative{old, other} {
		nativeRevoked(t, pool, grant, "version_mismatch")
		if _, err := service.AuthenticateAccess(context.Background(), grant.access.wire()); !errors.Is(err, ErrNativeUnauthorized) {
			t.Fatal("old family access survived")
		}
		if _, err := service.RotateRefresh(context.Background(), grant.refresh.wire()); !errors.Is(err, ErrNativeUnauthorized) {
			t.Fatal("old refresh survived")
		}
	}
	if _, err := service.AuthenticateAccess(context.Background(), fresh.access.wire()); err != nil {
		t.Fatal("current replacement cannot authenticate")
	}
	if _, _, err := Login(context.Background(), pool, user.Username, testPassword); err == nil {
		t.Fatal("old password survived")
	}
	if _, version, err := Login(context.Background(), pool, user.Username, nativeNextPassword); err != nil || version != proof.tokenVersion+1 {
		t.Fatal("new password cannot authenticate")
	}
	nativeCounts(t, pool, 3, 3, 3)
}

func TestNativePasswordConcurrentChangesSingleWinner(t *testing.T) {
	_, _, service, proof := nativeFixture(t)
	a, b := nativeIssue(t, service, proof), nativeIssue(t, service, proof)
	control := &nativePasswordConcurrentObserver{}
	type reply struct {
		grant IssuedNative
		err   error
	}
	replies := make(chan reply, 2)
	start := make(chan struct{})
	for _, grant := range []IssuedNative{a, b} {
		go func(g IssuedNative) {
			<-start
			next, err := service.changePasswordAndReissue(context.Background(), g.Principal(), testPassword, nativeNextPassword, control)
			replies <- reply{next, err}
		}(grant)
	}
	close(start)
	success := 0
	for range 2 {
		r := <-replies
		if r.err == nil {
			success++
			if _, err := service.AuthenticateAccess(context.Background(), r.grant.access.wire()); err != nil {
				t.Fatal("winner grant invalid")
			}
		} else if !errors.Is(r.err, ErrNativeUnauthorized) {
			t.Fatal("concurrent stale change not unauthorized")
		}
	}
	if success != 1 || control.calls.Load() != 1 {
		t.Fatal("multiple credential writes or stale loser cutoff")
	}
}

func TestNativePasswordRollbackAndLostCommitAcknowledgement(t *testing.T) {
	for _, point := range []string{"UPDATE users SET password_hash", "UPDATE native_session_families SET revoked_at", "INSERT INTO native_session_families", "INSERT INTO native_access_tokens", "INSERT INTO native_refresh_tokens"} {
		t.Run(point, func(t *testing.T) {
			pool, user, service, proof := nativeFixture(t)
			grant := nativeIssue(t, service, proof)
			fault := nativeFaultService(t, pool, &authQueryHook{match: point, cancel: true})
			control := &nativePasswordObserver{}
			next, err := fault.changePasswordAndReissue(context.Background(), grant.Principal(), testPassword, nativeNextPassword, control)
			if err == nil || next.Principal().User().ID != uuid.Nil {
				t.Fatal("failed transaction exposed a grant")
			}
			if _, version, err := Login(context.Background(), pool, user.Username, testPassword); err != nil || version != proof.tokenVersion {
				t.Fatal("failed transaction changed password/version")
			}
			if _, err := service.AuthenticateAccess(context.Background(), grant.access.wire()); err != nil {
				t.Fatal("rollback changed persistent session")
			}
			want := 1
			if point == "UPDATE users SET password_hash" {
				want = 0
			}
			if control.calls != want {
				t.Fatal("cutoff inconsistent with possible committed write")
			}
			nativeCounts(t, pool, 1, 1, 1)
		})
	}
	t.Run("commit cancellation", func(t *testing.T) {
		pool, user, service, proof := nativeFixture(t)
		grant := nativeIssue(t, service, proof)
		service.begin = func(ctx context.Context) (pgx.Tx, error) {
			tx, err := pool.Begin(ctx)
			return nativePasswordCanceledCommitTx{tx}, err
		}
		control := &nativePasswordObserver{}
		next, err := service.changePasswordAndReissue(context.Background(), grant.Principal(), testPassword, nativeNextPassword, control)
		if !errors.Is(err, context.Canceled) || next != (IssuedNative{}) || control.calls != 1 {
			t.Fatal("commit cancellation did not preserve safe empty result/cause/cutoff")
		}
		if _, version, err := Login(context.Background(), pool, user.Username, testPassword); err != nil || version != proof.tokenVersion {
			t.Fatal("canceled commit changed password/version")
		}
		nativeCounts(t, pool, 1, 1, 1)
	})
	t.Run("lost acknowledgement", func(t *testing.T) {
		pool, user, service, proof := nativeFixture(t)
		grant := nativeIssue(t, service, proof)
		service.begin = func(ctx context.Context) (pgx.Tx, error) {
			tx, err := pool.Begin(ctx)
			return nativeLostCommitTx{Tx: tx}, err
		}
		control := &nativePasswordObserver{}
		next, err := service.changePasswordAndReissue(context.Background(), grant.Principal(), testPassword, nativeNextPassword, control)
		if err == nil || next.Principal().User().ID != uuid.Nil || control.calls != 1 {
			t.Fatal("uncertain commit leaked a grant or left old transports")
		}
		if _, _, err := Login(context.Background(), pool, user.Username, nativeNextPassword); err != nil {
			t.Fatal("committed new password cannot recover via login")
		}
		if _, err := service.AuthenticateAccess(context.Background(), grant.access.wire()); !errors.Is(err, ErrNativeUnauthorized) {
			t.Fatal("old access survived uncertain commit")
		}
		nativeCounts(t, pool, 2, 2, 2)
	})
}

func TestNativePasswordRechecksConcurrentDisableAndExpiresDuringWrite(t *testing.T) {
	for _, disable := range []bool{true, false} {
		t.Run(map[bool]string{true: "disable", false: "expiry"}[disable], func(t *testing.T) {
			pool, user, service, proof := nativeFixture(t)
			grant := nativeIssue(t, service, proof)
			if !disable {
				if _, err := pool.Exec(context.Background(), `UPDATE native_access_tokens SET expires_at=clock_timestamp()+interval '1 second' WHERE family_id=$1`, grant.Principal().FamilyID()); err != nil {
					t.Fatal(err)
				}
				principal, err := service.AuthenticateAccess(context.Background(), grant.access.wire())
				if err != nil {
					t.Fatal(err)
				}
				grant.principal = principal
			}
			control := &nativePasswordObserver{}
			if disable {
				lock, err := pool.Begin(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				defer lock.Rollback(context.Background())
				if _, err := lock.Exec(context.Background(), `UPDATE users SET disabled_at=clock_timestamp() WHERE id=$1`, user.ID); err != nil {
					t.Fatal(err)
				}
				waiting := make(chan struct{})
				fault := nativeFaultService(t, pool, &authQueryHook{match: "FROM users WHERE id=$1 FOR UPDATE", before: func() { close(waiting) }})
				done := make(chan error, 1)
				go func() {
					_, err := fault.changePasswordAndReissue(context.Background(), grant.Principal(), testPassword, nativeNextPassword, control)
					done <- err
				}()
				select {
				case <-waiting:
				case <-time.After(2 * time.Second):
					t.Fatal("change did not reach held user lock")
				}
				if err := lock.Commit(context.Background()); err != nil {
					t.Fatal(err)
				}
				if err := <-done; !errors.Is(err, ErrNativeUnauthorized) {
					t.Fatal("concurrent disable accepted")
				}
				if control.calls != 0 {
					t.Fatal("pre-write disabled account cutoff")
				}
			} else {
				fault := nativeFaultService(t, pool, &authQueryHook{match: "INSERT INTO native_refresh_tokens", before: func() { time.Sleep(1200 * time.Millisecond) }})
				if _, err := fault.changePasswordAndReissue(context.Background(), grant.Principal(), testPassword, nativeNextPassword, control); !errors.Is(err, ErrNativeUnauthorized) {
					t.Fatal("expired credential mutation committed")
				}
				if _, version, err := Login(context.Background(), pool, user.Username, testPassword); err != nil || version != proof.tokenVersion {
					t.Fatal("expired mutation changed credentials")
				}
				if control.calls != 1 {
					t.Fatal("postwrite expiry did not conservatively retire")
				}
			}
			nativeCounts(t, pool, 1, 1, 1)
		})
	}
}

func TestNativePasswordCapturedHashAccessAndFamilyCannotBeRebound(t *testing.T) {
	for _, change := range []string{"hash", "access", "family", "user"} {
		t.Run(change, func(t *testing.T) {
			pool, user, service, proof := nativeFixture(t)
			grant := nativeIssue(t, service, proof)
			newHash, err := HashPassword(nativeNextPassword)
			if err != nil {
				t.Fatal(err)
			}
			fault := nativeFaultService(t, pool, &authQueryHook{match: "FROM users WHERE id=$1 FOR UPDATE", before: func() {
				var err error
				switch change {
				case "hash":
					_, err = pool.Exec(context.Background(), `UPDATE users SET password_hash=$1 WHERE id=$2`, newHash, user.ID)
				case "access":
					_, err = pool.Exec(context.Background(), `UPDATE native_access_tokens SET expires_at=expires_at-interval '1 second' WHERE family_id=$1`, grant.Principal().FamilyID())
				case "family":
					_, err = pool.Exec(context.Background(), `DELETE FROM native_session_families WHERE id=$1`, grant.Principal().FamilyID())
				case "user":
					_, err = pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, user.ID)
				}
				if err != nil {
					t.Fatal("concurrent fixture mutation failed")
				}
			}})
			control := &nativePasswordObserver{}
			next, err := fault.changePasswordAndReissue(context.Background(), grant.Principal(), testPassword, nativeNextPassword, control)
			if !errors.Is(err, ErrNativeUnauthorized) || next != (IssuedNative{}) || control.calls != 0 {
				t.Fatal("changed captured authorization or hash was rebound")
			}
		})
	}
}

func TestNativePasswordStoreFailuresCancellationAndEntropyAreRedacted(t *testing.T) {
	for _, query := range []string{"FROM native_access_tokens a JOIN native_session_families", "SELECT password_hash FROM users WHERE id=$1 AND", "FROM users WHERE id=$1 FOR UPDATE", "FROM native_session_families WHERE id=$1", "SELECT password_hash FROM users WHERE id=$1", "SELECT expires_at FROM native_access_tokens", "SELECT clock_timestamp()"} {
		t.Run(query, func(t *testing.T) {
			pool, _, service, proof := nativeFixture(t)
			grant := nativeIssue(t, service, proof)
			fault := nativeFaultService(t, pool, &authQueryHook{match: query, cancel: true})
			control := &nativePasswordObserver{}
			next, err := fault.changePasswordAndReissue(context.Background(), grant.Principal(), testPassword, nativeNextPassword, control)
			if err == nil || next != (IssuedNative{}) || control.calls != 0 {
				t.Fatal("pre-write fault returned credentials or retired current transports")
			}
			if strings.Contains(err.Error(), grant.access.wire()) || strings.Contains(err.Error(), testPassword) {
				t.Fatal("secret-bearing error")
			}
		})
	}
	for _, query := range []string{`SELECT password_hash FROM users WHERE id=$1`, `SELECT expires_at FROM native_access_tokens WHERE token_hash=$1 AND family_id=$2`} {
		t.Run("locked query "+query, func(t *testing.T) {
			pool, _, service, proof := nativeFixture(t)
			grant := nativeIssue(t, service, proof)
			service.begin = func(ctx context.Context) (pgx.Tx, error) {
				tx, err := pool.Begin(ctx)
				return nativePasswordFaultQueryTx{Tx: tx, query: query}, err
			}
			control := &nativePasswordObserver{}
			next, err := service.changePasswordAndReissue(context.Background(), grant.Principal(), testPassword, nativeNextPassword, control)
			if !errors.Is(err, context.Canceled) || next != (IssuedNative{}) || control.calls != 0 {
				t.Fatal("locked query failure advanced credential state")
			}
		})
	}
	t.Run("captured instance mismatch", func(t *testing.T) {
		_, _, service, proof := nativeFixture(t)
		grant := nativeIssue(t, service, proof)
		principal := grant.Principal()
		principal.instanceID = uuid.New()
		control := &nativePasswordObserver{}
		if next, err := service.changePasswordAndReissue(context.Background(), principal, testPassword, nativeNextPassword, control); !errors.Is(err, ErrNativeUnauthorized) || next != (IssuedNative{}) || control.calls != 0 {
			t.Fatal("foreign instance replaced credentials")
		}
	})
	t.Run("disabled after authentication before hash lookup", func(t *testing.T) {
		pool, user, service, proof := nativeFixture(t)
		grant := nativeIssue(t, service, proof)
		fault := nativeFaultService(t, pool, &authQueryHook{match: "SELECT password_hash FROM users WHERE id=$1 AND", before: func() {
			if _, err := pool.Exec(context.Background(), `UPDATE users SET disabled_at=clock_timestamp() WHERE id=$1`, user.ID); err != nil {
				t.Fatal("fixture disable failed")
			}
		}})
		control := &nativePasswordObserver{}
		if next, err := fault.changePasswordAndReissue(context.Background(), grant.Principal(), testPassword, nativeNextPassword, control); !errors.Is(err, ErrNativeUnauthorized) || next != (IssuedNative{}) || control.calls != 0 {
			t.Fatal("pre-hash disabled account changed credentials")
		}
	})
	for _, size := range []int{0, 32, 64} {
		t.Run("entropy"+string(rune('A'+size)), func(t *testing.T) {
			_, _, service, proof := nativeFixture(t)
			grant := nativeIssue(t, service, proof)
			input := make([]byte, size)
			for i := range input {
				input[i] = byte(i + 1)
			}
			service.random = bytes.NewReader(input)
			control := &nativePasswordObserver{}
			next, err := service.changePasswordAndReissue(context.Background(), grant.Principal(), testPassword, nativeNextPassword, control)
			if !errors.Is(err, errNativeEntropy) || next != (IssuedNative{}) || control.calls != 0 {
				t.Fatal("entropy failure did not fail closed")
			}
		})
	}
	t.Run("cancelled context", func(t *testing.T) {
		_, _, service, proof := nativeFixture(t)
		grant := nativeIssue(t, service, proof)
		control := &nativePasswordObserver{}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		next, err := service.changePasswordAndReissue(ctx, grant.Principal(), testPassword, nativeNextPassword, control)
		if !errors.Is(err, context.Canceled) || next != (IssuedNative{}) || control.calls != 0 {
			t.Fatal("cancelled operation advanced credential ownership")
		}
	})
}

func TestNativePasswordActualTLSStrictBodySharedLockoutAndNewGrant(t *testing.T) {
	_, user, h := nativeHTTPFixture(t)
	control := &nativePasswordObserver{}
	h.accounts.Live = control
	router, err := h.PasswordChangeHandler()
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(router)
	defer server.Close()
	grant := nativeHTTPLogin(t, h, user)
	request := func(body string, want int) {
		r, err := http.NewRequest(http.MethodPut, server.URL+"/auth/password", strings.NewReader(body))
		if err != nil {
			t.Fatal("request construction failed")
		}
		r.Header.Set("Authorization", "Bearer "+grant.Access)
		r.Header.Set("Content-Type", "application/json")
		response, err := server.Client().Do(r)
		if err != nil {
			t.Fatal("verified TLS request failed")
		}
		defer response.Body.Close()
		data, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal("response read failed")
		}
		if response.StatusCode != want || response.Header.Get("Cache-Control") != "no-store" || len(response.Cookies()) != 0 {
			t.Fatal("password response status/cache/cookie mismatch")
		}
		if want == http.StatusOK {
			var next nativeClientGrant
			if json.Unmarshal(data, &next) != nil || len(next.Access) != 43 || next.Access == grant.Access {
				t.Fatal("fresh native grant missing")
			}
			nativeStatus(t, h.Handler(), nativeAuthedRequest(http.MethodGet, "/auth/me", "", next.Access), http.StatusOK)
		}
	}
	for _, body := range []string{`{}`, `null`, `{"current_password":"x","new_password":null}`, `{"current_password":"x","new_password":"synthetic-password","extra":"x"}`, `{"current_password":"x","current_password":"y","new_password":"synthetic-password"}`, `{"current_password":"x","new_password":"synthetic-password"} {}`} {
		request(body, http.StatusBadRequest)
	}
	request(strings.Repeat("x", int(nativeAuthMaxBody)+1), http.StatusRequestEntityTooLarge)
	request(`{"current_password":"synthetic-wrong","new_password":"short"}`, http.StatusBadRequest)
	// The actual browser handler records four account failures; native adds the
	// fifth and then shares the same lockout even for the correct password.
	for range 4 {
		r := nativeRequest(http.MethodPut, "/api/auth/password", `{"current_password":"synthetic-wrong","new_password":"synthetic-password"}`)
		r = r.WithContext(WithUser(r.Context(), user))
		if err := h.accounts.changePassword(httptest.NewRecorder(), r); !errors.Is(err, ErrWrongPassword) {
			t.Fatal("browser failure did not share account policy")
		}
	}
	request(`{"current_password":"synthetic-wrong","new_password":"synthetic-password"}`, http.StatusForbidden)
	request(`{"current_password":"`+testPassword+`","new_password":"`+nativeNextPassword+`"}`, http.StatusTooManyRequests)
	h.accounts.passwordFails.ResetFailures(user.ID.String())
	request(`{"current_password":"`+testPassword+`","new_password":"`+nativeNextPassword+`"}`, http.StatusOK)
	if control.calls != 1 {
		t.Fatal("failed changes retired transports")
	}
}
