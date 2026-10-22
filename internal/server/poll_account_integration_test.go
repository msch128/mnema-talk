//go:build integration

package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/msch128/mnema-talk/internal/chat"
)

func TestAnExportHoldsTheMembersOwnPollVotesAndNobodyElses(t *testing.T) {
	a, _ := mailApp(t)
	owner := a.seedAdmin()
	f := pollFixture{a: a, owner: owner, alice: a.register(owner, "alice"), bob: a.register(owner, "bob"), channel: a.createChannel(owner, "general", "text")}
	m := f.basic(t, f.owner, map[string]any{"anonymous": true, "multiple": true})
	wantStatus(t, "alice votes", f.vote(f.alice, m, f.optionID(m, 0), f.optionID(m, 2)), http.StatusOK)
	wantStatus(t, "bob votes", f.vote(f.bob, m, f.optionID(m, 1)), http.StatusOK)

	wantStatus(t, "request", a.requestExport(f.alice, memberPassword, ""), http.StatusAccepted)
	if st := a.waitForExport(f.alice); st.State != "ready" {
		t.Fatalf("export: %+v", st)
	}
	files := unzipAll(t, f.alice.get("/api/users/me/export/download").body)
	var votes []struct {
		MessageID string `json:"message_id"`
		Option    string `json:"option"`
	}
	if err := json.Unmarshal([]byte(files["poll_votes.json"]), &votes); err != nil {
		t.Fatalf("poll_votes.json: %v: %q", err, files["poll_votes.json"])
	}
	if len(votes) != 2 || votes[0].MessageID != m.ID.String() || votes[0].Option != "Pizza" || votes[1].Option != "Both" {
		t.Fatalf("alice's votes: %+v", votes)
	}
	for name, content := range files {
		if strings.Contains(content, f.bob.user.ID.String()) || strings.Contains(content, "Pasta") {
			t.Errorf("%s holds something of bob's", name)
		}
	}
}

func TestDeletingAnAccountRemovesItsVotesAndThePollsItPosted(t *testing.T) {
	f := newPollFixture(t)
	theirs := f.basic(t, f.alice, nil)
	bobs := f.basic(t, f.bob, nil)
	wantStatus(t, "alice votes in bob's poll", f.vote(f.alice, bobs, f.optionID(bobs, 0)), http.StatusOK)
	wantStatus(t, "bob votes in bob's poll", f.vote(f.bob, bobs, f.optionID(bobs, 1)), http.StatusOK)
	wantStatus(t, "bob votes in alice's poll", f.vote(f.bob, theirs, f.optionID(theirs, 2)), http.StatusOK)

	wantStatus(t, "delete alice", f.a.deleteAccount(f.alice, memberPassword, ""), http.StatusNoContent)

	var msgs []chat.Message
	f.bob.get(f.base() + "/messages").decode(t, &msgs)
	byID := map[string]chat.Message{}
	for _, m := range msgs {
		byID[m.ID.String()] = m
	}
	if p := byID[theirs.ID.String()].Poll; p != nil {
		t.Fatalf("the deleted member's poll is still there: %+v", p)
	}
	p := byID[bobs.ID.String()].Poll
	if p == nil || !sameInts(tallies(*p), []int{0, 1, 0}) || p.TotalVoters != 1 {
		t.Fatalf("bob's poll after alice left: %+v", p)
	}
	var rows int
	if err := f.a.db.QueryRow(t.Context(), `SELECT COUNT(*) FROM poll_votes WHERE user_id = $1`, f.alice.user.ID).Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("votes of the deleted member: %d, %v", rows, err)
	}
}
