//go:build integration

package communityevents

import (
	"context"
	"strings"
	"testing"
	"time"
)

// A sign-up that meets a cancellation being written waits for it and is refused.
func TestASignUpWaitsForACancelBeingWritten(t *testing.T) {
	f := newRosterFixture(t)
	roles := f.setRoles(t, role("Tank", 5))
	ctx := context.Background()
	a := f.user(t, "a", "user")

	tx, err := f.p.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE events SET status = 'cancelled' WHERE id = $1`, f.event.ID); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, _, err := f.s.SignUp(ctx, f.event.ID, a.ID, roles[0].ID)
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("the sign-up must wait for the cancellation, it returned %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err == nil || !strings.Contains(err.Error(), "takes no more sign-ups") {
		t.Fatalf("after the cancellation the sign-up is refused, got %v", err)
	}
	if f.state(t, a) != "none" {
		t.Fatal("nothing was written")
	}
}
