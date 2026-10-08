package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/msch128/mnema-talk/internal/db"
)

func TestNativeSessionPolicyRejectsUnboundedValues(t *testing.T) {
	config, err := pgxpool.ParseConfig("postgres://test:test@localhost/test")
	if err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	wrapped := &db.Pool{Pool: pool}
	valid := NativePolicy{SessionLifetime: 30 * 24 * time.Hour, FamilyLifetime: 30 * 24 * time.Hour}
	service, err := NewNativeSessions(wrapped, valid)
	if err != nil || service == nil {
		t.Fatalf("valid policy: %v", err)
	}
	for _, policy := range []NativePolicy{{}, {SessionLifetime: time.Hour}, {SessionLifetime: time.Second, FamilyLifetime: time.Second},
		{SessionLifetime: 366 * 24 * time.Hour, FamilyLifetime: time.Hour},
		{SessionLifetime: time.Hour, FamilyLifetime: 2 * time.Hour},
		{SessionLifetime: 365 * 24 * time.Hour, FamilyLifetime: 31 * 24 * time.Hour},
		{SessionLifetime: time.Hour, FamilyLifetime: time.Nanosecond}} {
		if _, err := NewNativeSessions(wrapped, policy); err == nil {
			t.Fatal("bad policy accepted")
		}
	}
	for _, pool := range []*db.Pool{nil, {}} {
		if _, err := NewNativeSessions(pool, valid); err == nil {
			t.Fatal("missing pool accepted")
		}
	}
	// Input validation must happen before any database access.
	if _, err := service.IssueVerified(context.Background(), verifiedNativeLogin{}, uuid.New()); err != ErrNativeUnauthorized {
		t.Fatal(err)
	}
	if _, err := service.IssueVerified(context.Background(), verifiedNativeLogin{userID: uuid.New(), tokenVersion: -1}, uuid.New()); err != ErrNativeUnauthorized {
		t.Fatal(err)
	}
	if _, err := service.IssueVerified(context.Background(), verifiedNativeLogin{userID: uuid.New()}, uuid.Nil); err != ErrNativeUnauthorized {
		t.Fatal(err)
	}
	if _, err := service.AuthenticateAccess(context.Background(), "invalid"); err != ErrNativeUnauthorized {
		t.Fatal(err)
	}
	if _, err := service.RotateRefresh(context.Background(), "invalid"); err != ErrNativeUnauthorized {
		t.Fatal(err)
	}
	if err := service.RevokeFamily(context.Background(), NativePrincipal{}); err != ErrNativeUnauthorized {
		t.Fatal(err)
	}
	for _, batch := range []int{0, -1, 1001} {
		if _, err := service.Cleanup(context.Background(), batch); err == nil {
			t.Fatal("bad cleanup batch accepted")
		}
	}
}

type nativeRollbackFailureTx struct{ pgx.Tx }

func (nativeRollbackFailureTx) Rollback(context.Context) error {
	return errors.New("synthetic rollback detail")
}

func TestNativeTransactionPreservesRollbackFailureWithoutLeakingDetails(t *testing.T) {
	service := &NativeSessions{begin: func(context.Context) (pgx.Tx, error) { return nativeRollbackFailureTx{}, nil }}
	err := service.transact(context.Background(), func(context.Context, pgx.Tx) error { return ErrNativeUnauthorized })
	if !errors.Is(err, ErrNativeUnauthorized) {
		t.Fatal("original auth outcome lost")
	}
	var storeError nativeStoreError
	if !errors.As(err, &storeError) {
		t.Fatal("rollback failure lost")
	}
	if err.Error() != "native session unauthorized\nnative session storage failed" {
		t.Fatal("unsafe or incomplete error")
	}
}
