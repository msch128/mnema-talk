//go:build integration

package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/emoji"
)

// The 1.0 features each broadcast or decode on every call, so they have a budget of their own,
// answered like every other limit (429 RATE_LIMITED with Retry-After).

func wantRateLimited(t *testing.T, what string, res response) {
	t.Helper()
	if res.status != http.StatusTooManyRequests || res.header.Get("Retry-After") == "" || !strings.Contains(string(res.body), "RATE_LIMITED") {
		t.Fatalf("%s: %d %s (Retry-After %q)", what, res.status, res.body, res.header.Get("Retry-After"))
	}
}

func TestStartingPollsHasABudgetPerMember(t *testing.T) {
	f := newPollFixture(t)
	body := map[string]any{"question": "Again?", "options": []string{"Yes", "No"}}
	for i := 0; i < 10; i++ {
		wantStatus(t, "poll", f.alice.post(f.base()+"/polls", body), http.StatusCreated)
	}
	wantRateLimited(t, "eleventh poll", f.alice.post(f.base()+"/polls", body))
	// Another member has their own budget.
	wantStatus(t, "poll by bob", f.bob.post(f.base()+"/polls", body), http.StatusCreated)
}

func TestPinningHasABudgetPerMember(t *testing.T) {
	f := newMessageModerationFixture(t, false)
	id := f.a.send(f.user, f.channel, "pin me")
	path := f.path("/messages/" + id.String() + "/pin")
	for i := 0; i < chat.PinChangesPerMinute; i++ {
		method := http.MethodPut
		if i%2 == 1 {
			method = http.MethodDelete
		}
		if res := f.mod.do(method, path, nil, nil); res.status != http.StatusOK {
			t.Fatalf("change %d: %d %s", i, res.status, res.body)
		}
	}
	wantRateLimited(t, "pin over budget", f.mod.do(http.MethodPut, path, nil, nil))
}

func TestUploadingEmojiHasABudgetPerMember(t *testing.T) {
	_, _, manager := emojiFixture(t, nil)
	data := squarePNG(t, 32)
	for i := 0; i < emoji.UploadsPer10Minutes; i++ {
		// Names repeat, so most uploads are refused as taken: they still count against the budget.
		res := uploadEmoji(manager, "party", "x.png", "image/png", data)
		if res.status != http.StatusCreated && res.status != http.StatusConflict {
			t.Fatalf("upload %d: %d %s", i, res.status, res.body)
		}
	}
	wantRateLimited(t, "upload over budget", uploadEmoji(manager, "party", "x.png", "image/png", data))
}
