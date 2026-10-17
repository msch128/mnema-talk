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
