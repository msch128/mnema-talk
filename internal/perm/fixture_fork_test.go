package perm

import (
	"testing"

	"github.com/google/uuid"
)

func TestAForkedFixtureChangesWithoutTouchingTheOriginal(t *testing.T) {
	f := NewFixture()
	ch := f.Channel(f.Category())
	user := uuid.New()

	forked := f.Fork().MemberOverwrite(true, ch, user, 0, ViewChannel)

	if !f.Snapshot().Channel(Subject{ID: user}, ch).Has(ViewChannel) {
		t.Fatal("the original lost access through the fork")
	}
	if forked.Snapshot().Channel(Subject{ID: user}, ch).Has(ViewChannel) {
		t.Fatal("the fork kept access it was denied")
	}
}

func TestAssignOnAForkDoesNotLeakIntoTheOriginal(t *testing.T) {
	f := NewFixture()
	user := uuid.New()
	// Three roles leave spare capacity in the backing array.
	for range 3 {
		f.Assign(user, f.Role(1, 0))
	}
	a, b := f.Role(2, 0), f.Role(3, 0)

	forked := f.Fork()
	f.Assign(user, a)
	forked.Assign(user, b)

	if got := f.s.userRoles[user]; got[len(got)-1] != a {
		t.Fatal("the fork's Assign overwrote the original's role")
	}
	if got := forked.s.userRoles[user]; got[len(got)-1] != b {
		t.Fatal("the original's Assign overwrote the fork's role")
	}
}
