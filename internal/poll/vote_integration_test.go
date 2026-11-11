//go:build integration

package poll

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat/messages"
	"github.com/msch128/mnema-talk/internal/db"
)

func TestVotingReportsDatabaseFailuresInsteadOfHidingThem(t *testing.T) {
	f := newFixture(t)
	m := f.newPoll(t)
	option := m.Poll.Options[0].ID
	ctx := context.Background()
	cases := map[string]struct {
		ddl []string
		run func(p *db.Pool) error
	}{
		"poll lookup":     {[]string{brokenPolls}, func(p *db.Pool) error { return Vote(ctx, p, m.ID, f.user.ID, []uuid.UUID{option}) }},
		"option check":    {[]string{brokenOptions}, func(p *db.Pool) error { return Vote(ctx, p, m.ID, f.user.ID, []uuid.UUID{option}) }},
		"clearing votes":  {[]string{brokenVotes}, func(p *db.Pool) error { return Vote(ctx, p, m.ID, f.user.ID, []uuid.UUID{option}) }},
		"storing a vote":  {[]string{refusingVotes}, func(p *db.Pool) error { return Vote(ctx, p, m.ID, f.user.ID, []uuid.UUID{option}) }},
		"reading votes":   {[]string{brokenVotes}, func(p *db.Pool) error { _, err := MyVotes(ctx, p, m.ID, f.user.ID); return err }},
		"finding a poll":  {[]string{keylessPolls}, func(p *db.Pool) error { _, err := loadRef(ctx, p, m.ID); return err }},
		"ending a poll":   {[]string{brokenPolls}, func(p *db.Pool) error { _, err := end(ctx, p, m.ID); return err }},
		"announcing":      {[]string{brokenPolls}, func(p *db.Pool) error { _, err := f.service(p).announce(ctx, f.channel, m.ID); return err }},
		"reading details": {[]string{brokenPolls}, func(p *db.Pool) error { _, err := f.service(p).detail(ctx, m.ID, f.user.ID); return err }},
		"reading my part": {[]string{brokenVotes}, func(p *db.Pool) error { _, err := f.service(p).detail(ctx, m.ID, f.user.ID); return err }},
	}
	for name, c := range cases {
		if err := c.run(drift(t, f.p, c.ddl...)); !notAPollError(err) {
			t.Errorf("%s: error = %v", name, err)
		}
	}
	// None of it changed the poll.
	got, err := messages.LoadPoll(ctx, f.p, m.ID)
	if err != nil || got.TotalVoters != 0 {
		t.Fatalf("poll after the failures: %+v, %v", got, err)
	}
}
