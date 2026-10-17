//go:build integration

package ws

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/testutil"
)

func TestVoiceTimeWritesAreDebouncedAndFlushedOnClose(t *testing.T) {
	p := testutil.DB(t)
	u := wsFixtureUser(t, p)
	h := NewHub(p, nil, nil, nil)
	total := func() int64 {
		var n int64
		if err := p.QueryRow(context.Background(), `SELECT voice_seconds FROM users WHERE id=$1`, u.ID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	ch := uuid.New()
	h.addVoiceTime(u.ID, ch, 3*time.Second) // the first write is immediate
	if total() != 3 {
		t.Fatalf("total = %d, want 3", total())
	}
	h.addVoiceTime(u.ID, ch, 4*time.Second) // inside the window: held back
	h.addVoiceTime(u.ID, ch, 5*time.Second)
	if total() != 3 {
		t.Fatalf("total = %d after held stays, want 3", total())
	}
	h.Close() // flushes what is held
	if total() != 12 {
		t.Fatalf("total = %d after close, want 12", total())
	}
}

func TestVoiceTimeFlushWritesWhatIsHeld(t *testing.T) {
	p := testutil.DB(t)
	u := wsFixtureUser(t, p)
	h := NewHub(p, nil, nil, nil)
	h.addVoiceTime(u.ID, uuid.New(), time.Second)
	h.addVoiceTime(u.ID, uuid.New(), 2*time.Second)
	h.flushVoiceTime(u.ID)
	var n int64
	if err := p.QueryRow(context.Background(), `SELECT voice_seconds FROM users WHERE id=$1`, u.ID).Scan(&n); err != nil || n != 3 {
		t.Fatalf("total = %d, %v", n, err)
	}
	h.flushVoiceTime(u.ID) // nothing left: no write
	h.flushVoiceTime(uuid.New())
}

// Held time of one Talk is never announced as part of another Talk's stay.
func TestVoiceTimeIsAnnouncedOnlyToTheTalkItWasSpentIn(t *testing.T) {
	p := testutil.DB(t)
	u := wsFixtureUser(t, p)
	h := NewHub(p, nil, nil, nil)
	type seen struct {
		ch    uuid.UUID
		total int64
	}
	var got []seen
	h.Tap = func(ch uuid.UUID, eventType string, payload any) {
		if eventType != "user_stats" {
			return
		}
		got = append(got, seen{ch, payload.(map[string]any)["voice_seconds"].(int64)})
	}
	hidden, visible := uuid.New(), uuid.New()
	h.addVoiceTime(u.ID, hidden, 3*time.Second)  // written at once
	h.addVoiceTime(u.ID, hidden, 4*time.Second)  // held back
	h.addVoiceTime(u.ID, visible, 5*time.Second) // another Talk: the held 4 s go out first, to the hidden Talk
	h.addVoiceTime(u.ID, visible, 6*time.Second) // held back in the visible Talk
	h.flushVoiceTime(u.ID)
	want := []seen{{hidden, 3}, {hidden, 7}, {visible, 18}}
	if len(got) != len(want) {
		t.Fatalf("announcements = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("announcement %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}
