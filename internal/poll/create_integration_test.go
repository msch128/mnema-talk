//go:build integration

package poll

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestAPollThatCannotBeStoredLeavesNoMessageBehind(t *testing.T) {
	f := newFixture(t)
	for name, broken := range map[string]string{"poll row": brokenPolls, "options": brokenOptions} {
		p := drift(t, f.p, broken)
		_, err := f.service(p).Create(context.Background(), f.user, f.channel, Input{Question: "Pizza?", Options: []string{"Yes", "No"}})
		if !notAPollError(err) {
			t.Fatalf("%s: error = %v", name, err)
		}
		if n := f.messages(t); n != 0 {
			t.Fatalf("%s: %d messages left behind", name, n)
		}
	}
}

func TestCreateRefusesBadInputAndMissingChannels(t *testing.T) {
	f := newFixture(t)
	s := f.service(f.p)
	if _, err := s.Create(context.Background(), f.user, f.channel, Input{Question: "only a question"}); err == nil {
		t.Fatal("a poll without answers was accepted")
	}
	if _, err := s.Create(context.Background(), f.user, uuid.New(), Input{Question: "Pizza?", Options: []string{"Yes", "No"}}); err == nil {
		t.Fatal("a poll in a channel that does not exist was accepted")
	}
	if n := f.messages(t); n != 0 {
		t.Fatalf("%d messages left behind", n)
	}
}
