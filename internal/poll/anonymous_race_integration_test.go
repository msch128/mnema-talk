//go:build integration

package poll

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
)

// Members voting at the same moment in one anonymous poll must all be
// counted: each vote share-locks the poll row and then raises its counter, so
// two of them waiting on each other's lock must not end in a deadlock.
func TestManyMembersVotingAtOnceInAnAnonymousPollAreAllCounted(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	msg, err := f.service(f.p).Create(ctx, f.user, f.channel, Input{Question: "Pizza?", Options: []string{"Yes", "No"}, Anonymous: true})
	if err != nil {
		t.Fatal(err)
	}
	var option uuid.UUID
	if err := f.p.QueryRow(ctx, `SELECT id FROM poll_options WHERE poll_id = $1 ORDER BY position LIMIT 1`, msg.ID).Scan(&option); err != nil {
		t.Fatal(err)
	}
	const voters = 12
	ids := make([]uuid.UUID, voters)
	for i := range ids {
		if err := f.p.QueryRow(ctx, `INSERT INTO users (username, display_name, password_hash) VALUES ($1, 'v', 'test-only-unused') RETURNING id`,
			"voter"+uuid.NewString()[:8]).Scan(&ids[i]); err != nil {
			t.Fatal(err)
		}
	}
	errs := make([]error, voters)
	var wg sync.WaitGroup
	for i := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = Vote(ctx, f.p, msg.ID, ids[i], []uuid.UUID{option})
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("voter %d: %v", i, err)
		}
	}
	var counted int
	if err := f.p.QueryRow(ctx, `SELECT anon_voters FROM polls WHERE message_id = $1`, msg.ID).Scan(&counted); err != nil {
		t.Fatal(err)
	}
	if counted != voters {
		t.Fatalf("%d of %d votes counted", counted, voters)
	}
}
