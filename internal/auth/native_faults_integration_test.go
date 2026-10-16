//go:build integration

package auth

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/testutil/pgfault"
)

// A database failure while reading the captured password hash is a store
// failure: it must not issue a grant or touch the account.
func TestNativePasswordChangeReportsStoreFailureReadingHash(t *testing.T) {
	pool, user, service, proof := nativeFixture(t)
	grant := nativeIssue(t, service, proof)
	fault := nativeFaultService(t, pool, &authQueryHook{match: "SELECT password_hash FROM users WHERE id=$1 AND token_version=$2", cancel: true})
	control := &nativePasswordObserver{}
	next, err := fault.changePasswordAndReissue(context.Background(), grant.Principal(), testPassword, nativeNextPassword, control)
	if err == nil || errors.Is(err, ErrNativeUnauthorized) || next != (IssuedNative{}) || control.calls != 0 {
		t.Fatalf("hash read failure: grant=%v calls=%d err=%v", next != (IssuedNative{}), control.calls, err)
	}
	if _, version, err := Login(context.Background(), pool, user.Username, testPassword); err != nil || version != proof.tokenVersion {
		t.Fatal("failed hash read changed password/version")
	}
}

// Cleanup stops with a store failure when the candidate stream breaks after
// the query started, and leaves every family alone.
func TestNativeCleanupReportsBrokenCandidateStream(t *testing.T) {
	pool, _, service, proof := nativeFixture(t)
	grant := nativeIssue(t, service, proof)
	if err := service.RevokeFamily(context.Background(), grant.Principal()); err != nil {
		t.Fatal(err)
	}
	tracer := &pgfault.Tracer{Query: "FROM native_session_families f JOIN users u", AfterPrepare: true}
	fault, err := NewNativeSessions(&db.Pool{Pool: pgfault.Pool(t, pool.Pool, tracer)}, NativePolicy{SessionLifetime: nativeMaxFamilyTTL, FamilyLifetime: nativeMaxFamilyTTL})
	if err != nil {
		t.Fatal(err)
	}
	if n, err := fault.Cleanup(context.Background(), 10); err == nil || n != 0 {
		t.Fatalf("cleanup with broken stream: n=%d err=%v", n, err)
	}
	var families int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM native_session_families WHERE id <> $1`, uuid.Nil).Scan(&families); err != nil || families != 1 {
		t.Fatalf("failed cleanup removed families: count=%d err=%v", families, err)
	}
}
