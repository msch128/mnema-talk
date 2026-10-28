//go:build integration

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/poll"
)

func (f pollFixture) vote(c *client, m chat.Message, ids ...uuid.UUID) response {
	if ids == nil {
		ids = []uuid.UUID{}
	}
	return c.put(f.base()+"/messages/"+m.ID.String()+"/poll/votes", map[string]any{"option_ids": ids})
}

func (f pollFixture) detail(t *testing.T, c *client, m chat.Message) poll.Detail {
	t.Helper()
	res := c.get(f.base() + "/messages/" + m.ID.String() + "/poll")
	wantStatus(t, "get poll", res, http.StatusOK)
	var d poll.Detail
	res.decode(t, &d)
	return d
}

func (f pollFixture) optionID(m chat.Message, i int) uuid.UUID { return m.Poll.Options[i].ID }

func tallies(p chat.Poll) []int {
	out := make([]int, len(p.Options))
	for i, o := range p.Options {
		out[i] = o.Votes
	}
	return out
}

func sameInts(a, b []int) bool { return fmt.Sprint(a) == fmt.Sprint(b) }

func TestSingleChoiceVoteIsReplacedAndWithdrawn(t *testing.T) {
	f := newPollFixture(t)
	m := f.basic(t, f.alice, nil)

	wantStatus(t, "first vote", f.vote(f.bob, m, f.optionID(m, 0)), http.StatusOK)
	wantStatus(t, "another member", f.vote(f.alice, m, f.optionID(m, 0)), http.StatusOK)
	res := f.vote(f.bob, m, f.optionID(m, 2))
	wantStatus(t, "change vote", res, http.StatusOK)
	var d poll.Detail
	res.decode(t, &d)
	if !sameInts(tallies(d.Poll), []int{1, 0, 1}) || d.TotalVoters != 2 || len(d.MyVotes) != 1 || d.MyVotes[0] != f.optionID(m, 2) {
		t.Fatalf("after change: %+v", d)
	}
	wantStatus(t, "two answers in a single-choice poll", f.vote(f.bob, m, f.optionID(m, 0), f.optionID(m, 1)), http.StatusBadRequest)

	res = f.vote(f.bob, m)
	wantStatus(t, "withdraw", res, http.StatusOK)
	res.decode(t, &d)
	if !sameInts(tallies(d.Poll), []int{1, 0, 0}) || d.TotalVoters != 1 || len(d.MyVotes) != 0 {
		t.Fatalf("after withdrawing: %+v", d)
	}
	wantStatus(t, "unknown option", f.vote(f.bob, m, uuid.New()), http.StatusBadRequest)
	wantStatus(t, "same option twice", f.vote(f.bob, m, f.optionID(m, 0), f.optionID(m, 0)), http.StatusBadRequest)
}

func TestMultipleChoiceVotesAreASet(t *testing.T) {
	f := newPollFixture(t)
	m := f.basic(t, f.alice, map[string]any{"multiple": true})
	res := f.vote(f.bob, m, f.optionID(m, 0), f.optionID(m, 2))
	wantStatus(t, "two answers", res, http.StatusOK)
	var d poll.Detail
	res.decode(t, &d)
	if !sameInts(tallies(d.Poll), []int{1, 0, 1}) || d.TotalVoters != 1 || len(d.MyVotes) != 2 {
		t.Fatalf("poll: %+v", d)
	}
	wantStatus(t, "narrow down", f.vote(f.bob, m, f.optionID(m, 1)), http.StatusOK)
	if got := f.detail(t, f.bob, m); !sameInts(tallies(got.Poll), []int{0, 1, 0}) {
		t.Fatalf("poll: %+v", got)
	}
}

func TestPollVotesNeedAccessAndAnOpenPoll(t *testing.T) {
	f := newPollFixture(t)
	m := f.basic(t, f.alice, nil)
	option := f.optionID(m, 0)

	wantStatus(t, "no session", f.vote(f.a.anon(), m, option), http.StatusUnauthorized)

	// A member who cannot react (a timeout takes this away) cannot vote.
	role := createRole(t, f.owner, "Silenced", 0)
	assignRoles(t, f.owner, f.bob, role)
	wantStatus(t, "deny reactions", putOverwrites(f.owner, "channels", f.channel, overwrite("role", role, 0, perm.AddReactions)), http.StatusNoContent)
	wantStatus(t, "cannot react", f.vote(f.bob, m, option), http.StatusForbidden)
	wantStatus(t, "others can", f.vote(f.alice, m, option), http.StatusOK)
	wantStatus(t, "may still read it", f.bob.get(f.base()+"/messages/"+m.ID.String()+"/poll"), http.StatusOK)

	// Hidden channel: the poll does not exist.
	wantStatus(t, "hide", putOverwrites(f.owner, "channels", f.channel, overwrite("role", everyoneID(t, f.owner), 0, perm.ViewChannel)), http.StatusNoContent)
	wantStatus(t, "vote in hidden channel", f.vote(f.alice, m, option), http.StatusNotFound)
	wantStatus(t, "read in hidden channel", f.alice.get(f.base()+"/messages/"+m.ID.String()+"/poll"), http.StatusNotFound)
	wantStatus(t, "end in hidden channel", f.alice.post(f.base()+"/messages/"+m.ID.String()+"/poll/end", nil), http.StatusNotFound)

	// The poll of one channel is not reachable through another.
	other := f.a.createChannel(f.owner, "other", "text")
	wantStatus(t, "wrong channel", f.owner.get("/api/channels/"+other.String()+"/messages/"+m.ID.String()+"/poll"), http.StatusNotFound)
	// A plain message is no poll.
	plain := f.a.send(f.owner, other, "hello")
	wantStatus(t, "not a poll", f.owner.get("/api/channels/"+other.String()+"/messages/"+plain.String()+"/poll"), http.StatusNotFound)
}

func TestPollEndsEarlyByAuthorOrModeratorAndThenRefusesVotes(t *testing.T) {
	f := newPollFixture(t)
	m := f.basic(t, f.alice, nil)
	end := f.base() + "/messages/" + m.ID.String() + "/poll/end"
	wantStatus(t, "vote", f.vote(f.bob, m, f.optionID(m, 1)), http.StatusOK)

	wantStatus(t, "a bystander cannot end it", f.bob.post(end, nil), http.StatusForbidden)
	mod := createRole(t, f.owner, "Mods", perm.ManageMessages)
	assignRoles(t, f.owner, f.bob, mod)
	before := len(f.a.events.Snapshot())
	res := f.bob.post(end, nil)
	wantStatus(t, "a moderator can", res, http.StatusOK)
	var d poll.Detail
	res.decode(t, &d)
	if !d.Closed || len(d.MyVotes) != 1 {
		t.Fatalf("after ending: %+v", d)
	}
	wantStatus(t, "vote after the end", f.vote(f.alice, m, f.optionID(m, 0)), http.StatusConflict)
	wantStatus(t, "withdraw after the end", f.vote(f.bob, m), http.StatusConflict)

	updates := 0
	for _, e := range f.a.events.Snapshot()[before:] {
		if e.Type == events.PollUpdate && e.ChannelID == f.channel {
			updates++
		}
	}
	if updates != 1 {
		t.Fatalf("ending broadcast %d poll_update events", updates)
	}
	wantStatus(t, "ending again", f.alice.post(end, nil), http.StatusOK)
	if got := len(f.a.events.Snapshot()); got != before+1 {
		t.Fatalf("ending twice published %d events", got-before)
	}

	// The tally stays and the message list says it is closed.
	var msgs []chat.Message
	f.alice.get(f.base()+"/messages").decode(t, &msgs)
	if p := msgs[0].Poll; !p.Closed || !sameInts(tallies(*p), []int{0, 1, 0}) {
		t.Fatalf("history: %+v", p)
	}
}

func TestPollClosesByItselfWhenTheTimeIsUp(t *testing.T) {
	f := newPollFixture(t)
	m := f.basic(t, f.alice, nil)
	if _, err := f.a.db.Exec(t.Context(), `UPDATE polls SET closes_at = NOW() - INTERVAL '1 second' WHERE message_id = $1`, m.ID); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, "vote", f.vote(f.bob, m, f.optionID(m, 0)), http.StatusConflict)
	if d := f.detail(t, f.bob, m); !d.Closed {
		t.Fatalf("poll: %+v", d)
	}
}

func TestVotingBroadcastsTalliesToTheChannel(t *testing.T) {
	f := newPollFixture(t)
	m := f.basic(t, f.alice, map[string]any{"multiple": true})
	wantStatus(t, "vote", f.vote(f.bob, m, f.optionID(m, 0), f.optionID(m, 1)), http.StatusOK)
	var last *poll.Update
	for _, e := range f.a.events.Snapshot() {
		if u, ok := e.Payload.(poll.Update); ok && e.Type == events.PollUpdate && e.ChannelID == f.channel {
			last = &u
		}
	}
	if last == nil || last.MessageID != m.ID || !sameInts(tallies(last.Poll), []int{1, 1, 0}) || last.Poll.TotalVoters != 1 {
		t.Fatalf("poll_update: %+v", last)
	}
	if v := last.Poll.Options[0].Voters; len(v) != 1 || v[0] != f.bob.user.ID {
		t.Fatalf("an open poll names its voters: %+v", last.Poll.Options[0])
	}
}

// An anonymous poll never lets a voter id out: not in the message, the list,
// the poll route, the broadcast, a search hit or the mentions inbox, not even
// for the author or an administrator; a member learns only that they voted.
func TestAnonymousPollNeverRevealsWhoVoted(t *testing.T) {
	f := newPollFixture(t)
	m := f.basic(t, f.alice, map[string]any{"anonymous": true, "question": "Secret pineapple ballot"})
	wantStatus(t, "bob votes", f.vote(f.bob, m, f.optionID(m, 0)), http.StatusOK)
	wantStatus(t, "owner votes", f.vote(f.owner, m, f.optionID(m, 1)), http.StatusOK)

	forbidden := []string{f.bob.user.ID.String(), f.owner.user.ID.String()}
	assertNoVoters := func(what string, body []byte) {
		t.Helper()
		for _, id := range forbidden {
			if strings.Contains(string(body), id) {
				t.Fatalf("%s names voter %s: %s", what, id, body)
			}
		}
		if strings.Contains(string(body), `"voters"`) {
			t.Fatalf("%s has a voters list: %s", what, body)
		}
	}
	for name, c := range map[string]*client{"author": f.alice, "other member": f.bob, "owner": f.owner} {
		assertNoVoters(name+" history", c.get(f.base()+"/messages").body)
		assertNoVoters(name+" mentions", c.get("/api/mentions").body)
	}
	assertNoVoters("author's poll route", f.alice.get(f.base()+"/messages/"+m.ID.String()+"/poll").body)
	assertNoVoters("owner's poll route", f.owner.get(f.base()+"/messages/"+m.ID.String()+"/poll").body)
	assertNoVoters("search", f.alice.get("/api/search?q=pineapple").body)
	assertNoVoters("by-number lookup", f.alice.get(fmt.Sprintf("/api/messages/by-number/%d", m.Number)).body)
	for _, e := range f.a.events.Snapshot() {
		if e.Type == events.PollUpdate || e.Type == events.MessageCreate {
			raw, err := json.Marshal(e.Payload)
			if err != nil {
				t.Fatal(err)
			}
			assertNoVoters("event "+e.Type, raw)
		}
	}
	// The server does not know the choice, only that bob took part; the
	// running tally stays hidden (see TestRunningAnonymousPollHidesTheTallyUntilItEnds).
	if d := f.detail(t, f.bob, m); len(d.MyVotes) != 0 || !d.HasVoted || !sameInts(tallies(d.Poll), []int{0, 0, 0}) || d.TotalVoters != 2 {
		t.Fatalf("bob's own view: %+v", d)
	}
	if d := f.detail(t, f.alice, m); len(d.MyVotes) != 0 {
		t.Fatalf("the author did not vote but sees %v", d.MyVotes)
	}
}

func TestVotingRacesNeverBreakTheRules(t *testing.T) {
	f := newPollFixture(t)
	single := f.basic(t, f.alice, nil)
	const workers = 24
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res := f.vote(f.bob, single, f.optionID(single, i%3))
			if res.status != http.StatusOK && res.status != http.StatusTooManyRequests {
				t.Errorf("vote %d: %d %s", i, res.status, res.body)
			}
		}()
	}
	wg.Wait()
	d := f.detail(t, f.bob, single)
	sum := 0
	for _, n := range tallies(d.Poll) {
		sum += n
	}
	if sum != 1 || d.TotalVoters != 1 || len(d.MyVotes) != 1 {
		t.Fatalf("one member double-voted a single-choice poll: %+v", d)
	}

	// Many members at once: every vote counts exactly once.
	crowd := f.basic(t, f.alice, nil)
	members := make([]*client, 6)
	for i := range members {
		members[i] = f.a.register(f.owner, fmt.Sprintf("voter%d", i))
	}
	for round := 0; round < 2; round++ {
		for i, c := range members {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if res := f.vote(c, crowd, f.optionID(crowd, i%3)); res.status != http.StatusOK {
					t.Errorf("member %d: %d %s", i, res.status, res.body)
				}
			}()
		}
	}
	wg.Wait()
	if got := f.detail(t, f.owner, crowd); !sameInts(tallies(got.Poll), []int{2, 2, 2}) || got.TotalVoters != 6 {
		t.Fatalf("crowd: %+v", got.Poll)
	}
}

func TestVotingAndEndingRaceLeavesNoLateVote(t *testing.T) {
	f := newPollFixture(t)
	for round := 0; round < 3; round++ {
		m := f.basic(t, f.alice, nil)
		end := f.base() + "/messages/" + m.ID.String() + "/poll/end"
		var wg sync.WaitGroup
		statuses := make([]int, 6)
		for i := range statuses {
			wg.Add(1)
			go func() {
				defer wg.Done()
				statuses[i] = f.vote(f.bob, m, f.optionID(m, i%3)).status
			}()
		}
		wg.Add(1)
		go func() { defer wg.Done(); f.alice.post(end, nil) }()
		wg.Wait()
		// Once ended, a vote is refused; whatever got in before is final.
		wantStatus(t, "after the end", f.vote(f.bob, m, f.optionID(m, 0)), http.StatusConflict)
		before := f.detail(t, f.bob, m)
		wantStatus(t, "still ended", f.vote(f.bob, m, f.optionID(m, 1)), http.StatusConflict)
		if after := f.detail(t, f.bob, m); !sameInts(tallies(before.Poll), tallies(after.Poll)) {
			t.Fatalf("a vote landed after the end: %v -> %v", tallies(before.Poll), tallies(after.Poll))
		}
		if before.TotalVoters > 1 {
			t.Fatalf("round %d: %+v", round, before)
		}
	}
}
