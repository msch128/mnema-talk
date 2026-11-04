//go:build integration

package communityevents

import (
	"context"
	"testing"
	"time"
)

// SignUp takes the event row, then the role rows and last the member's RSVP.
// Changing the RSVP must take them in the same order, or a member who answers
// in one tab while signing up in another makes the two wait for each other.
func TestChangingTheRSVPWhileASignUpHoldsTheRoleLocksDoesNotDeadlock(t *testing.T) {
	ctx := context.Background()
	f := newRosterFixture(t)
	f.setRoles(t, role("Tank", 1))
	a := f.user(t, "a", "user")
	if _, err := f.s.SetRSVP(ctx, f.event.ID, a.ID, RSVPInterested); err != nil {
		t.Fatal(err)
	}

	tx, err := f.p.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := openEvent(ctx, tx, f.event.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := lockRoles(ctx, tx, f.event.ID); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := f.s.SetRSVP(ctx, f.event.ID, a.ID, RSVPNotGoing)
		done <- err
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var waiting int
		if err := f.p.QueryRow(ctx, `SELECT COUNT(*) FROM pg_locks WHERE NOT granted`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the RSVP change did not wait for the role locks")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// SignUp's last step, while the RSVP change waits.
	err = markGoing(ctx, tx, f.event.ID, a.ID)
	if err == nil {
		err = tx.Commit(ctx)
	}
	if err != nil {
		t.Fatalf("the sign-up failed: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("the RSVP change failed: %v", err)
	}
}
