//go:build integration

package automod

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
)

// Lane 7 of the defect hunt, probe of the shape of H-L7-06: restrictJoiners
// counts the recent joiners in a subquery of the statement that takes
// `FOR UPDATE` of the state row, so a call that waited for the lock counts on
// the snapshot from before the wait. The raid is still found, because the call
// that started last has the newest snapshot and every other joiner is covered
// by the UPDATE of the call that starts the raid (a separate statement).
func TestAJoinThatWaitedForTheRaidLockDoesNotHideTheRaid(t *testing.T) {
	p := newStore(t)
	ctx := context.Background()
	e := New(p, perm.NewService(p), nil, nil)
	first := newUser(t, p, "first_user")
	if _, err := p.Exec(ctx, `INSERT INTO automod_state DEFAULT VALUES ON CONFLICT DO NOTHING`); err != nil {
		t.Fatal(err)
	}
	raid := &compiled{Rule: Rule{Name: "Raid", Kind: KindRaid, Config: Settings{Joins: 1, WindowSeconds: 600, RestrictSeconds: 300}}}

	hold, err := p.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = hold.Rollback(ctx) }()
	if _, err := hold.Exec(ctx, `SELECT 1 FROM automod_state FOR UPDATE`); err != nil {
		t.Fatal(err)
	}
	type outcome struct {
		started bool
		err     error
		took    time.Duration
	}
	run := func(uid uuid.UUID) chan outcome {
		ch := make(chan outcome, 1)
		go func() {
			began := time.Now()
			_, started, err := e.restrictJoiners(ctx, raid, uid)
			ch <- outcome{started, err, time.Since(began)}
		}()
		return ch
	}
	firstDone := run(first)
	time.Sleep(300 * time.Millisecond) // the first call waits with a count of one joiner
	second := newUser(t, p, "second_user")
	secondDone := run(second)
	time.Sleep(300 * time.Millisecond)
	if err := hold.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	a, b := <-firstDone, <-secondDone
	if a.err != nil || b.err != nil {
		t.Fatalf("errors: %v %v", a.err, b.err)
	}
	if a.took < 250*time.Millisecond || b.took < 250*time.Millisecond {
		t.Fatalf("the calls took %v and %v: they did not wait, the interleaving did not happen", a.took, b.took)
	}
	if a.started || !b.started {
		t.Errorf("started: first %v, second %v; want the second call (newest snapshot) to begin the raid", a.started, b.started)
	}
	for name, id := range map[string]uuid.UUID{"first": first, "second": second} {
		if until, _, _ := timeoutOf(t, p, id); !until.After(time.Now()) {
			t.Errorf("the %s joiner is not restricted", name)
		}
	}
}
