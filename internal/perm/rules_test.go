package perm

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/httpx"
)

func TestPendingRulesTakeWhatATimeoutTakes(t *testing.T) {
	f := NewFixture()
	ch := f.Channel(uuid.Nil)
	pending, accepted, stale, owner, bot, admin := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	f.Owner(owner).Bot(bot).Assign(admin, f.Role(5, Administrator))
	f.Assign(bot, f.Role(2, SendMessages|ViewChannel|Connect))
	f.Rules(2).AcceptedRules(accepted, 2).AcceptedRules(stale, 1)
	s := f.Snapshot()

	for name, id := range map[string]uuid.UUID{"never accepted": pending, "old version": stale, "admin": admin} {
		got := s.Channel(Subject{ID: id}, ch)
		if !got.Has(ViewChannel) || got.HasAny(TimeoutMask) {
			t.Fatalf("%s holds %b", name, got)
		}
		if s.Global(Subject{ID: id}).HasAny(TimeoutMask) || !s.RulesPending(id) {
			t.Fatalf("%s: global %b, pending %v", name, s.Global(Subject{ID: id}), s.RulesPending(id))
		}
	}
	for name, id := range map[string]uuid.UUID{"accepted": accepted, "owner": owner, "bot": bot} {
		if s.RulesPending(id) {
			t.Fatalf("%s must not be pending", name)
		}
	}
	if !s.Channel(Subject{ID: accepted}, ch).Has(SendMessages | AddReactions | Connect) {
		t.Fatal("a member who accepted keeps their rights")
	}
	if !s.Channel(Subject{ID: bot}, ch).Has(SendMessages|Connect) || s.Channel(Subject{ID: owner}, ch) != All {
		t.Fatal("bots and the Owner are exempt")
	}
}

func TestNoRulesEnforcesNothing(t *testing.T) {
	f := NewFixture()
	ch := f.Channel(uuid.Nil)
	member := uuid.New()
	s := f.Rules(0).Snapshot()
	if s.RulesPending(member) || !s.Channel(Subject{ID: member}, ch).Has(SendMessages) {
		t.Fatal("without rules nobody is restricted")
	}
}

func TestDenyNamesTheRulesNotAMissingPermission(t *testing.T) {
	f := NewFixture()
	ch := f.Channel(uuid.Nil)
	pending, plain := uuid.New(), uuid.New()
	f.Rules(1).AcceptedRules(plain, 1)
	f.RoleOverwrite(true, ch, f.Snapshot().everyoneID, 0, SendMessages)
	s := f.Snapshot()

	sub := Subject{ID: pending}
	p := s.Channel(sub, ch)
	err := s.Deny(sub, p, ViewChannel|AddReactions)
	var api *httpx.APIError
	if !errors.As(err, &api) || api.Code != httpx.CodeRulesNotAccepted || api.Status != 403 {
		t.Fatalf("pending member: %v", err)
	}
	if err := s.Deny(sub, p, ViewChannel); err != nil {
		t.Fatalf("reading stays possible: %v", err)
	}
	// Someone who accepted but lacks the permission gets the ordinary refusal.
	psub := Subject{ID: plain}
	err = s.Deny(psub, s.Channel(psub, ch), SendMessages)
	if !errors.As(err, &api) || api.Code != httpx.CodeForbidden {
		t.Fatalf("accepted member without the right: %v", err)
	}
}
