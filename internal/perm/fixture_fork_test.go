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
