//go:build integration

package chat

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat/dm"
)

func TestGroupLimitCountsAreSerializedPerAddedMember(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	bob, carol := newMember(t, f, "lock_bob"), newMember(t, f, "lock_carol")
	group, _, err := dm.CreateDM(ctx, f.p, dm.NewDM{Creator: f.u.ID, Others: []uuid.UUID{bob, carol}})
	if err != nil {
		t.Fatal(err)
	}
	dave := newMember(t, f, "lock_dave")
	hold, err := f.p.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = hold.Rollback(ctx) }()
	if err := dm.LockUsers(ctx, hold, []uuid.UUID{dave}); err != nil {
		t.Fatal(err)
	}
	blocked := func(run func() error) {
		t.Helper()
		done := make(chan error, 1)
		go func() { done <- run() }()
		waitForBlockedStatement(t, f.p)
		select {
		case err := <-done:
			t.Fatalf("finished while the member's row was locked: %v", err)
		default:
		}
		if err := hold.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	blocked(func() error {
		added, err := dm.AddDMMember(ctx, f.p, group, f.u.ID, dave)
		if err == nil && !added {
			err = errors.New("not added")
		}
		return err
	})
	erin := newMember(t, f, "lock_erin")
	hold, err = f.p.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := dm.LockUsers(ctx, hold, []uuid.UUID{erin}); err != nil {
		t.Fatal(err)
	}
	blocked(func() error {
		_, _, err := dm.CreateDM(ctx, f.p, dm.NewDM{Creator: f.u.ID, Others: []uuid.UUID{erin, bob}})
		return err
	})
}
