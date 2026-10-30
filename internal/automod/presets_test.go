package automod

import (
	"fmt"
	"testing"

	"github.com/msch128/mnema-talk/internal/testutil/presets"
)

// Every choice of the rule editor is accepted when the rule is saved.
func TestClientAutomodPresetsPassValidation(t *testing.T) {
	set := presets.Load(t)
	base := func(kind string, cfg Settings, timeout int) RuleInput {
		return RuleInput{Kind: kind, Name: "Rule", Enabled: true, Action: ActionBlock, TimeoutSeconds: timeout, Config: cfg}
	}
	check := func(name string, in RuleInput) {
		if _, err := in.Normalize(); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	for _, s := range set.AutomodAccountAges {
		check(fmt.Sprintf("account age %d s", s), base(KindNewAccount, Settings{MaxAgeSeconds: s, Restrict: RestrictLinks}, 0))
	}
	for _, s := range set.AutomodRaidWindows {
		check(fmt.Sprintf("raid window %d s", s), base(KindRaid, Settings{Joins: 5, WindowSeconds: s, RestrictSeconds: 300}, 0))
	}
	for _, s := range set.AutomodRaidRestrictions {
		check(fmt.Sprintf("raid restriction %d s", s), base(KindRaid, Settings{Joins: 5, WindowSeconds: 60, RestrictSeconds: s}, 0))
	}
	for _, s := range set.AutomodRuleTimeouts {
		check(fmt.Sprintf("rule timeout %d s", s), base(KindMentions, Settings{MaxMentions: 5}, s))
	}
}
