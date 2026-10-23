//go:build integration

package server

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
)

func TestPollRoutesRefuseMalformedRequests(t *testing.T) {
	f := newPollFixture(t)
	m := f.basic(t, f.alice, nil)
	poll := f.base() + "/messages/" + m.ID.String() + "/poll"
	good := map[string]any{"question": "q", "options": []string{"a", "b"}}

	wantStatus(t, "bad channel id", f.alice.post("/api/channels/x/polls", good), http.StatusBadRequest)
	wantStatus(t, "unknown channel", f.alice.post("/api/channels/"+uuid.NewString()+"/polls", good), http.StatusNotFound)
	wantStatus(t, "not an object", f.alice.post(f.base()+"/polls", []string{"a"}), http.StatusBadRequest)
	wantStatus(t, "question missing", f.alice.post(f.base()+"/polls", map[string]any{"options": []string{"a", "b"}}), http.StatusBadRequest)
	wantStatus(t, "bad duration", f.alice.post(f.base()+"/polls", map[string]any{"question": "q", "options": []string{"a", "b"}, "duration": "forever"}), http.StatusBadRequest)

	wantStatus(t, "bad message id", f.alice.get(f.base()+"/messages/x/poll"), http.StatusBadRequest)
	wantStatus(t, "bad message id when voting", f.alice.put(f.base()+"/messages/x/poll/votes", map[string]any{"option_ids": []string{}}), http.StatusBadRequest)
	wantStatus(t, "bad channel id when ending", f.alice.post("/api/channels/x/messages/"+m.ID.String()+"/poll/end", nil), http.StatusBadRequest)
	wantStatus(t, "vote body is not JSON", f.bob.put(poll+"/votes", "nonsense"), http.StatusBadRequest)
	wantStatus(t, "option id is not an id", f.bob.put(poll+"/votes", map[string]any{"option_ids": []string{"x"}}), http.StatusBadRequest)
	wantStatus(t, "unknown field", f.bob.put(poll+"/votes", map[string]any{"option_ids": []string{}, "bogus": true}), http.StatusBadRequest)
	wantStatus(t, "unknown poll", f.bob.get(f.base()+"/messages/"+uuid.NewString()+"/poll"), http.StatusNotFound)
}

func TestVoteChangesAreRateLimitedPerMember(t *testing.T) {
	f := newPollFixture(t)
	m := f.basic(t, f.alice, nil)
	var refused response
	for i := 0; i < 70 && refused.status == 0; i++ {
		if res := f.vote(f.bob, m, f.optionID(m, i%3)); res.status == http.StatusTooManyRequests {
			refused = res
		} else {
			wantStatus(t, "vote", res, http.StatusOK)
		}
	}
	if refused.status == 0 || refused.header.Get("Retry-After") == "" {
		t.Fatalf("a flood of votes was never slowed down: %+v", refused)
	}
	// Somebody else is not held back by it.
	wantStatus(t, "another member", f.vote(f.alice, m, f.optionID(m, 0)), http.StatusOK)
}

func TestPollsFollowSlowMode(t *testing.T) {
	f := newPollFixture(t)
	wantStatus(t, "slow mode", f.owner.patch("/api/admin/channels/"+f.channel.String(), map[string]any{"slow_mode_seconds": 60}), http.StatusOK)
	f.basic(t, f.alice, nil)
	res := f.alice.post(f.base()+"/polls", map[string]any{"question": "again?", "options": []string{"a", "b"}})
	wantStatus(t, "second poll within the wait", res, http.StatusTooManyRequests)
	if res.header.Get("Retry-After") == "" {
		t.Fatal("no Retry-After on slow mode")
	}
	wantStatus(t, "slash command is held back too", slashPoll(f.alice, f.channel, map[string]any{"question": "q", "answers": "a|b"}), http.StatusTooManyRequests)
	f.basic(t, f.owner, nil) // moderators are exempt
}
