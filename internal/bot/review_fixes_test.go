package bot

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestInteractionReleaseGivesBackTheSlot(t *testing.T) {
	s := &interactionStore{m: map[uuid.UUID]*interaction{}, now: time.Now}
	bot, id := uuid.New(), uuid.New()
	token, ok := s.add(id, &interaction{botID: bot})
	if !ok {
		t.Fatal("add")
	}
	if _, err := s.claim(id, token, bot, true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.claim(id, token, bot, true); err == nil {
		t.Fatal("the first response is taken")
	}
	s.release(id, true)
	if _, err := s.claim(id, token, bot, true); err != nil {
		t.Fatalf("after release: %v", err)
	}

	// A follow-up that failed does not use up one of the responses.
	for i := 0; i < maxResponses*3; i++ {
		if _, err := s.claim(id, token, bot, false); err != nil {
			t.Fatalf("follow-up %d: %v", i, err)
		}
		s.release(id, false)
	}
	if got := s.m[id].responses; got != 1 {
		t.Fatalf("responses = %d, want 1", got)
	}
	s.release(uuid.New(), true) // unknown ids are ignored
}

func TestInteractionRemove(t *testing.T) {
	s := &interactionStore{m: map[uuid.UUID]*interaction{}, now: time.Now}
	id := uuid.New()
	s.add(id, &interaction{botID: uuid.New()})
	s.remove(id)
	if len(s.m) != 0 {
		t.Fatal("remove keeps the interaction")
	}
}

func TestCommandColumnsAs(t *testing.T) {
	want := "c.id, c.bot_id, c.name, c.description, c.options, c.disabled, c.created_at"
	if got := commandColumnsAs("c"); got != want {
		t.Fatalf("got %q", got)
	}
}
