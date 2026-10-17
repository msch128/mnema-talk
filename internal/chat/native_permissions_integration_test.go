//go:build integration

package chat

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
)

func TestNativeCiphertextFollowsChannelPermissions(t *testing.T) {
	f, _, store, principal := nativeContentFixture(t)
	ctx := context.Background()
	svc := perm.NewService(f.p)
	guarded := store.WithPermissions(svc)
	request := OpaqueCiphertextRequest{f.c.ID, uuid.New(), []byte("fixture-group"), []byte{1, 2, 3}}
	if _, _, err := guarded.Publish(ctx, principal, request); err != nil {
		t.Fatalf("a member may publish by default: %v", err)
	}
	if _, err := guarded.List(ctx, principal, f.c.ID, 0, 10); err != nil {
		t.Fatalf("a member may list by default: %v", err)
	}

	var everyone uuid.UUID
	if err := f.p.QueryRow(ctx, `SELECT id FROM roles WHERE is_everyone`).Scan(&everyone); err != nil {
		t.Fatal(err)
	}
	deny := func(bits perm.Set) {
		t.Helper()
		if _, err := f.p.Exec(ctx, `DELETE FROM permission_overwrites`); err != nil {
			t.Fatal(err)
		}
		if _, err := f.p.Exec(ctx, `INSERT INTO permission_overwrites (channel_id, role_id, deny) VALUES ($1, $2, $3)`, f.c.ID, everyone, int64(bits)); err != nil {
			t.Fatal(err)
		}
		svc.Invalidate()
	}
	deny(perm.SendMessages)
	retry := OpaqueCiphertextRequest{f.c.ID, uuid.New(), []byte("fixture-group"), []byte{4}}
	if _, _, err := guarded.Publish(ctx, principal, retry); !errors.Is(err, ErrNativeContentChannel) {
		t.Fatalf("publishing without Send messages: %v", err)
	}
	if _, err := guarded.List(ctx, principal, f.c.ID, 0, 10); err != nil {
		t.Fatalf("reading stays possible: %v", err)
	}
	deny(perm.ViewChannel)
	if _, err := guarded.List(ctx, principal, f.c.ID, 0, 10); !errors.Is(err, ErrNativeContentChannel) {
		t.Fatalf("listing a hidden channel: %v", err)
	}
	if _, _, err := guarded.Publish(ctx, principal, retry); !errors.Is(err, ErrNativeContentChannel) {
		t.Fatalf("publishing to a hidden channel: %v", err)
	}
	// Without a permission service the single-server model stays.
	if _, err := store.WithPermissions(nil).List(ctx, principal, f.c.ID, 0, 10); err != nil {
		t.Fatalf("no permission service, no limits: %v", err)
	}
}

// A permission state that cannot be loaded fails closed, as unavailable.
func TestNativeCiphertextFailsClosedWithoutPermissions(t *testing.T) {
	f, _, store, principal := nativeContentFixture(t)
	p, ctx := cancelQueryPool(t, f.p, func(sql string) bool { return strings.Contains(sql, "FROM roles") }, false)
	guarded := store.WithPermissions(perm.NewService(p))
	if _, err := guarded.List(ctx, principal, f.c.ID, 0, 10); !errors.Is(err, ErrNativeContentUnavailable) {
		t.Fatalf("list with unloadable permissions: %v", err)
	}
}
