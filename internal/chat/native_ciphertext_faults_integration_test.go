//go:build integration

package chat

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/testutil/pgfault"
)

func TestNativeCiphertextStoreNeedsSessions(t *testing.T) {
	if store, err := NewNativeCiphertextStore(nil); store != nil || !errors.Is(err, ErrNativeContentInput) {
		t.Fatalf("nil sessions: %v, %v", store, err)
	}
}

// faultyNativeStore reads and writes through a pool whose first statement
// containing query is canceled; the principal comes from the healthy login.
func faultyNativeStore(t *testing.T, f chatFixture, tracer *pgfault.Tracer) *NativeCiphertextStore {
	t.Helper()
	sessions, err := auth.NewNativeSessions(&db.Pool{Pool: pgfault.Pool(t, f.p.Pool, tracer)}, auth.NativePolicy{SessionLifetime: 24 * time.Hour, FamilyLifetime: 24 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	store, err := NewNativeCiphertextStore(sessions)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

// A database failure while reading or writing opaque events is reported as
// the generic unavailable error, never as a channel or conflict result.
func TestNativeCiphertextDatabaseFailuresAreUnavailable(t *testing.T) {
	f, _, healthy, principal := nativeContentFixture(t)
	ctx := context.Background()
	request := OpaqueCiphertextRequest{ChannelID: f.c.ID, ClientEventID: uuid.New(), GroupID: []byte{1}, Ciphertext: []byte{2}}
	if _, created, err := healthy.Publish(ctx, principal, request); err != nil || !created {
		t.Fatalf("seed event: created=%v err=%v", created, err)
	}

	t.Run("publish channel lookup", func(t *testing.T) {
		store := faultyNativeStore(t, f, &pgfault.Tracer{Query: "SELECT id FROM channels WHERE id=$1 FOR UPDATE"})
		if _, _, err := store.Publish(ctx, principal, request); !errors.Is(err, ErrNativeContentUnavailable) {
			t.Fatalf("channel lookup failure: %v", err)
		}
	})
	t.Run("publish insert", func(t *testing.T) {
		fresh := request
		fresh.ClientEventID = uuid.New()
		store := faultyNativeStore(t, f, &pgfault.Tracer{Query: "INSERT INTO native_ciphertext_events"})
		if _, _, err := store.Publish(ctx, principal, fresh); !errors.Is(err, ErrNativeContentUnavailable) {
			t.Fatalf("insert failure: %v", err)
		}
	})
	t.Run("publish retry lookup", func(t *testing.T) {
		store := faultyNativeStore(t, f, &pgfault.Tracer{Query: "FROM native_ciphertext_events WHERE user_id=$1 AND client_event_id=$2"})
		if _, _, err := store.Publish(ctx, principal, request); !errors.Is(err, ErrNativeContentUnavailable) {
			t.Fatalf("retry lookup failure: %v", err)
		}
	})
	t.Run("list channel lookup", func(t *testing.T) {
		store := faultyNativeStore(t, f, &pgfault.Tracer{Query: "SELECT id FROM channels WHERE id=$1 FOR SHARE"})
		if _, err := store.List(ctx, principal, f.c.ID, 0, 10); !errors.Is(err, ErrNativeContentUnavailable) {
			t.Fatalf("list channel lookup failure: %v", err)
		}
	})
	t.Run("list query", func(t *testing.T) {
		store := faultyNativeStore(t, f, &pgfault.Tracer{Query: "FROM native_ciphertext_events WHERE channel_id=$1"})
		if _, err := store.List(ctx, principal, f.c.ID, 0, 10); !errors.Is(err, ErrNativeContentUnavailable) {
			t.Fatalf("list query failure: %v", err)
		}
	})
	t.Run("list row stream", func(t *testing.T) {
		store := faultyNativeStore(t, f, &pgfault.Tracer{Query: "FROM native_ciphertext_events WHERE channel_id=$1", AfterPrepare: true})
		if _, err := store.List(ctx, principal, f.c.ID, 0, 10); !errors.Is(err, ErrNativeContentUnavailable) {
			t.Fatalf("list row stream failure: %v", err)
		}
	})
}
