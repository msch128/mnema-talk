package perm

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestTimeoutTakesPostingAndSpeakingButNotReading(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	clock = func() time.Time { return now }
	t.Cleanup(func() { clock = time.Now })

	f := NewFixture()
	ch := f.Channel(uuid.Nil)
	member, mod, admin, owner := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	f.Owner(owner)
	f.Assign(admin, f.Role(5, Administrator))
	f.Assign(mod, f.Role(3, ManageMessages|ManageChannels))
	// An overwrite that explicitly allows posting must not undo the timeout.
	f.MemberOverwrite(true, ch, member, SendMessages|Speak, 0)
	until := now.Add(time.Hour)
	for _, u := range []uuid.UUID{member, mod, admin, owner} {
		f.Timeout(u, until, "calm down")
	}
	s := f.Snapshot()

	sub := Subject{ID: member}
	got := s.Channel(sub, ch)
	if !got.Has(ViewChannel) || got.HasAny(TimeoutMask) {
		t.Fatalf("timed out member holds %b", got)
	}
	if s.Global(sub).HasAny(TimeoutMask) || !s.Global(sub).Has(ViewChannel) {
		t.Fatalf("global %b", s.Global(sub))
	}
	if tm, ok := s.Timeout(member); !ok || tm.Reason != "calm down" || !tm.Until.Equal(until) {
		t.Fatalf("timeout %+v %v", tm, ok)
	}
	if len(s.Timeouts()) != 4 {
		t.Fatalf("timeouts %v", s.Timeouts())
	}

	// Moderation rights stay; the Administrator keeps everything but the mask.
	if m := s.Channel(Subject{ID: mod}, ch); !m.Has(ManageMessages) || m.HasAny(TimeoutMask) {
		t.Fatalf("moderator %b", m)
	}
	a := s.Channel(Subject{ID: admin}, ch)
	if !a.Has(Administrator|ManageRoles|ViewChannel) || a.HasAny(TimeoutMask) {
		t.Fatalf("administrator %b", a)
	}
	// The Owner cannot be timed out, whatever the table says.
	if o := s.Channel(Subject{ID: owner}, ch); o != All || s.Global(Subject{ID: owner}) != All {
		t.Fatalf("owner %b", o)
	}

	// It ends by itself, without a reload.
	now = until.Add(time.Second)
	if _, ok := s.Timeout(member); ok {
		t.Fatal("expired timeout still running")
	}
	if !s.Channel(sub, ch).Has(SendMessages|Speak) || len(s.Timeouts()) != 0 {
		t.Fatal("member did not get their rights back")
	}
}
