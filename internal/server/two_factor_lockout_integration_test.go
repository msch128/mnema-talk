//go:build integration

package server

import (
	"context"
	"net/http"
	"testing"
)

// While the second step is locked out, an attempt is turned away before it
// counts: it must not use up the challenge's five attempts, or the member
// would have to sign in again with the password once the lock ends.
func TestLockedOutSecondStepDoesNotSpendTheChallenge(t *testing.T) {
	a := newApp(t, false)
	alice := a.register(a.seedAdmin(), "alice")
	e := a.enroll(alice, memberPassword)
	a.freshStep()
	c := a.anon()

	// Ten wrong codes (two challenges of five attempts) lock the step.
	for i := 0; i < 2; i++ {
		ch := a.plantChallenge(alice.user.ID)
		for j := 0; j < 5; j++ {
			secondStep(c, ch, wrongCode(i*5+j, e.auth))
		}
	}
	ch := a.plantChallenge(alice.user.ID)
	for i := 0; i < 8; i++ {
		if res := secondStep(c, ch, e.auth.code(0)); res.status != http.StatusTooManyRequests {
			t.Fatalf("locked attempt %d: status %d, want 429", i, res.status)
		}
	}
	var attempts int
	if err := a.db.QueryRow(context.Background(), `SELECT attempts FROM login_challenges ORDER BY created_at DESC LIMIT 1`).Scan(&attempts); err != nil {
		t.Fatal(err)
	}
	if attempts != 0 {
		t.Fatalf("locked-out attempts were counted against the challenge: %d", attempts)
	}
}
