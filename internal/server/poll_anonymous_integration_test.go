//go:build integration

package server

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/poll"
)

// everyRowMentioning counts, over every table of the schema, the rows whose
// JSON form contains all the given strings. It is how the tests prove that no
// place anywhere keeps a member next to a choice, whatever the table is called.
func (a *app) everyRowMentioning(needles ...string) map[string]int {
	a.t.Helper()
	rows, err := a.db.Query(a.t.Context(), `SELECT table_name FROM information_schema.tables WHERE table_schema = current_schema() AND table_type = 'BASE TABLE'`)
	if err != nil {
		a.t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			a.t.Fatal(err)
		}
		tables = append(tables, name)
	}
	rows.Close()
	found := map[string]int{}
	for _, table := range tables {
		q := fmt.Sprintf(`SELECT COUNT(*) FROM %q t WHERE true`, table)
		args := make([]any, len(needles))
		for i, n := range needles {
			args[i] = n
			q += fmt.Sprintf(` AND to_jsonb(t)::text LIKE '%%' || $%d || '%%'`, i+1)
		}
		if n := a.countRows(q, args...); n > 0 {
			found[table] = n
		}
	}
	return found
}

func TestAnonymousPollStoresNoLinkBetweenAMemberAndAChoice(t *testing.T) {
	f := newPollFixture(t)
	m := f.basic(t, f.alice, map[string]any{"anonymous": true, "multiple": true})
	pizza, pasta, both := f.optionID(m, 0), f.optionID(m, 1), f.optionID(m, 2)
	wantStatus(t, "bob votes", f.vote(f.bob, m, pizza, both), http.StatusOK)
	wantStatus(t, "owner votes", f.vote(f.owner, m, pasta), http.StatusOK)

	d := f.detail(t, f.bob, m)
	if !sameInts(tallies(d.Poll), []int{1, 1, 1}) || d.TotalVoters != 2 || !d.HasVoted || len(d.MyVotes) != 0 {
		t.Fatalf("detail: %+v", d)
	}

	// Participation is stored, the choice is not.
	if n := f.a.countRows(`SELECT COUNT(*) FROM poll_voters WHERE poll_id = $1`, m.ID); n != 2 {
		t.Fatalf("poll_voters: %d rows", n)
	}
	if n := f.a.countRows(`SELECT COUNT(*) FROM poll_votes v JOIN poll_options o ON o.id = v.option_id WHERE o.poll_id = $1`, m.ID); n != 0 {
		t.Fatalf("poll_votes holds %d named votes of an anonymous poll", n)
	}
	if n := f.a.countRows(`SELECT COUNT(*) FROM information_schema.columns WHERE table_name = 'poll_voters' AND column_name NOT IN ('poll_id', 'user_id')`); n != 0 {
		t.Fatal("poll_voters grew a column that could tie a voter to a time or a choice")
	}

	// No table anywhere holds a voter next to any option of the poll.
	for _, voter := range []*client{f.bob, f.owner} {
		for _, opt := range []uuid.UUID{pizza, pasta, both} {
			if hits := f.a.everyRowMentioning(voter.user.ID.String(), opt.String()); len(hits) != 0 {
				t.Fatalf("voter %s and option %s share rows: %v", voter.user.ID, opt, hits)
			}
		}
	}

	// Nor does the audit log, even after the poll is ended and deleted.
	wantStatus(t, "end", f.owner.post(f.base()+"/messages/"+m.ID.String()+"/poll/end", nil), http.StatusOK)
	for _, opt := range []uuid.UUID{pizza, pasta, both} {
		if n := f.a.countRows(`SELECT COUNT(*) FROM audit_log WHERE details::text LIKE '%' || $1 || '%'`, opt.String()); n != 0 {
			t.Fatalf("%d audit entries name option %s", n, opt)
		}
	}
	// Ending someone else's poll is audited (poll.end), but never with a choice.
	if n := f.a.countRows(`SELECT COUNT(*) FROM audit_log WHERE details::text ~* '(voter|option|vote)'`); n != 0 {
		t.Fatalf("%d audit entries talk about votes", n)
	}
}

func TestAnonymousVoteCannotBeChangedOrWithdrawn(t *testing.T) {
	f := newPollFixture(t)
	m := f.basic(t, f.alice, map[string]any{"anonymous": true})
	wantStatus(t, "withdraw before voting", f.vote(f.bob, m), http.StatusBadRequest)
	wantStatus(t, "vote", f.vote(f.bob, m, f.optionID(m, 0)), http.StatusOK)
	wantStatus(t, "change", f.vote(f.bob, m, f.optionID(m, 1)), http.StatusConflict)
	wantStatus(t, "repeat", f.vote(f.bob, m, f.optionID(m, 0)), http.StatusConflict)
	wantStatus(t, "withdraw", f.vote(f.bob, m), http.StatusBadRequest)
	if d := f.detail(t, f.bob, m); !sameInts(tallies(d.Poll), []int{1, 0, 0}) || d.TotalVoters != 1 {
		t.Fatalf("counts moved: %+v", d)
	}
	// A refused vote must not leave a half-counted option behind.
	wantStatus(t, "unknown option", f.vote(f.owner, m, uuid.New()), http.StatusBadRequest)
	if n := f.a.countRows(`SELECT COUNT(*) FROM poll_voters WHERE poll_id = $1`, m.ID); n != 1 {
		t.Fatalf("a refused vote left %d voters", n)
	}
}

func TestAnonymousVotesRaceCountEachMemberOnce(t *testing.T) {
	f := newPollFixture(t)
	m := f.basic(t, f.alice, map[string]any{"anonymous": true})
	var wg sync.WaitGroup
	ok := make([]bool, 12)
	for i := range ok {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res := f.vote(f.bob, m, f.optionID(m, i%3))
			ok[i] = res.status == http.StatusOK
		}()
	}
	wg.Wait()
	wins := 0
	for _, v := range ok {
		if v {
			wins++
		}
	}
	d := f.detail(t, f.bob, m)
	if wins != 1 || d.TotalVoters != 1 || tallies(d.Poll)[0]+tallies(d.Poll)[1]+tallies(d.Poll)[2] != 1 {
		t.Fatalf("wins=%d %+v", wins, d)
	}
}

func TestADatabaseRefusesNamedVotesInAnAnonymousPoll(t *testing.T) {
	f := newPollFixture(t)
	m := f.basic(t, f.alice, map[string]any{"anonymous": true})
	_, err := f.a.db.Exec(t.Context(), `INSERT INTO poll_votes (option_id, user_id) VALUES ($1, $2)`, f.optionID(m, 0), f.bob.user.ID)
	if err == nil || !strings.Contains(err.Error(), "anonymous polls keep no named votes") {
		t.Fatalf("insert: %v", err)
	}
	open := f.basic(t, f.alice, nil)
	if _, err := f.a.db.Exec(t.Context(), `INSERT INTO poll_votes (option_id, user_id) VALUES ($1, $2)`, f.optionID(open, 0), f.bob.user.ID); err != nil {
		t.Fatalf("a named poll takes named votes: %v", err)
	}
}

func TestAnonymousPollIsOffTheMembersExportAndTheirErasureKeepsTheCounts(t *testing.T) {
	a, _ := mailApp(t)
	owner := a.seedAdmin()
	f := pollFixture{a: a, owner: owner, alice: a.register(owner, "alice"), bob: a.register(owner, "bob"), channel: a.createChannel(owner, "general", "text")}
	m := f.basic(t, f.owner, map[string]any{"anonymous": true})
	wantStatus(t, "alice votes", f.vote(f.alice, m, f.optionID(m, 0)), http.StatusOK)
	wantStatus(t, "bob votes", f.vote(f.bob, m, f.optionID(m, 1)), http.StatusOK)

	wantStatus(t, "request", a.requestExport(f.alice, memberPassword, ""), http.StatusAccepted)
	if st := a.waitForExport(f.alice); st.State != "ready" {
		t.Fatalf("export: %+v", st)
	}
	files := unzipAll(t, f.alice.get("/api/users/me/export/download").body)
	if got := strings.TrimSpace(files["poll_votes.json"]); got != "[]" && got != "null" && got != "" {
		t.Fatalf("an anonymous vote is in the export: %s", got)
	}

	wantStatus(t, "delete alice", a.deleteAccount(f.alice, memberPassword, ""), http.StatusNoContent)
	if n := a.countRows(`SELECT COUNT(*) FROM poll_voters WHERE user_id = $1`, f.alice.user.ID); n != 0 {
		t.Fatalf("poll_voters of the deleted member: %d", n)
	}
	var msgs []chat.Message
	f.bob.get(f.base()+"/messages").decode(t, &msgs)
	if len(msgs) != 1 || msgs[0].Poll == nil || !sameInts(tallies(*msgs[0].Poll), []int{1, 1, 0}) || msgs[0].Poll.TotalVoters != 2 {
		t.Fatalf("the counts changed with the erasure: %+v", msgs)
	}
}

// Polls that were anonymous before 1.2 stored (option, member) rows; the
// migration keeps the tallies and the participants and drops the choices.
func TestMigrationConvertsAnExistingAnonymousPoll(t *testing.T) {
	f := newPollFixture(t)
	m := f.basic(t, f.alice, map[string]any{"anonymous": true})
	named := f.basic(t, f.alice, nil)
	wantStatus(t, "named vote", f.vote(f.bob, named, f.optionID(named, 0)), http.StatusOK)

	ctx := t.Context()
	// Rebuild the state of a 1.1 database: named votes in the anonymous poll.
	for _, q := range []string{
		`ALTER TABLE poll_votes DISABLE TRIGGER poll_votes_not_anonymous`,
		`UPDATE polls SET anon_voters = 0 WHERE true`,
	} {
		if _, err := f.a.db.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	for _, v := range []struct {
		who *client
		opt int
	}{{f.bob, 0}, {f.owner, 0}, {f.alice, 2}} {
		if _, err := f.a.db.Exec(ctx, `INSERT INTO poll_votes (option_id, user_id) VALUES ($1, $2)`, f.optionID(m, v.opt), v.who.user.ID); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile("../db/migrations/0063_anonymous_polls_auto_delete.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(raw)
	start := strings.Index(sql, "-- Convert the anonymous polls")
	end := strings.Index(sql, "-- An anonymous poll never holds named votes.")
	if start < 0 || end < start {
		t.Fatal("conversion section of the migration not found")
	}
	if _, err := f.a.db.Exec(ctx, sql[start:end]); err != nil {
		t.Fatalf("conversion: %v", err)
	}

	d := f.detail(t, f.bob, m)
	if !sameInts(tallies(d.Poll), []int{2, 0, 1}) || d.TotalVoters != 3 || !d.HasVoted || len(d.MyVotes) != 0 {
		t.Fatalf("converted poll: %+v", d)
	}
	if n := f.a.countRows(`SELECT COUNT(*) FROM poll_votes v JOIN poll_options o ON o.id = v.option_id WHERE o.poll_id = $1`, m.ID); n != 0 {
		t.Fatalf("%d named votes survived", n)
	}
	if got := f.detail(t, f.bob, named); !sameInts(tallies(got.Poll), []int{1, 0, 0}) || len(got.MyVotes) != 1 {
		t.Fatalf("a named poll was touched: %+v", got)
	}
}

// A ballot carries neither a member nor a time, and folding the ballots into
// the counters changes no result.
func TestAnonymousBallotsHaveNoMemberAndFoldingChangesNothing(t *testing.T) {
	f := newPollFixture(t)
	m := f.basic(t, f.alice, map[string]any{"anonymous": true})
	wantStatus(t, "bob", f.vote(f.bob, m, f.optionID(m, 0)), http.StatusOK)
	wantStatus(t, "owner", f.vote(f.owner, m, f.optionID(m, 0)), http.StatusOK)

	if n := f.a.countRows(`SELECT COUNT(*) FROM information_schema.columns WHERE table_name = 'poll_anon_ballots' AND column_name NOT IN ('poll_id', 'option_id')`); n != 0 {
		t.Fatal("poll_anon_ballots has a column that could name a member or a time")
	}
	if n := f.a.countRows(`SELECT COUNT(*) FROM poll_anon_ballots WHERE poll_id = $1`, m.ID); n != 2 {
		t.Fatalf("unfolded ballots: %d", n)
	}
	before := tallies(f.detail(t, f.bob, m).Poll)
	if err := poll.FoldBallots(t.Context(), f.a.db); err != nil {
		t.Fatal(err)
	}
	if n := f.a.countRows(`SELECT COUNT(*) FROM poll_anon_ballots`); n != 0 {
		t.Fatalf("ballots left after the fold: %d", n)
	}
	after := f.detail(t, f.bob, m)
	if !sameInts(before, tallies(after.Poll)) || !sameInts(before, []int{2, 0, 0}) || after.TotalVoters != 2 {
		t.Fatalf("fold changed the result: %v -> %+v", before, after)
	}
	wantStatus(t, "third voter after a fold", f.vote(f.alice, m, f.optionID(m, 1)), http.StatusOK)
	if got := tallies(f.detail(t, f.bob, m).Poll); !sameInts(got, []int{2, 1, 0}) {
		t.Fatalf("after fold and vote: %v", got)
	}
}
