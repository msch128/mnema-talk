package mentions

import (
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat/core"
)

// matchRoles runs the role matching resolveMentionSet starts with.
func matchRoles(content string, roles []MentionRole) []MentionRole {
	return hitRoles(roleHits(content, roles))
}

func TestMatchRoles(t *testing.T) {
	dev := MentionRole{ID: uuid.New(), Name: "Dev"}
	team := MentionRole{ID: uuid.New(), Name: "Dev Team"}
	qa := MentionRole{ID: uuid.New(), Name: "QA"}
	roles := []MentionRole{dev, team, qa}
	names := func(in string) []string {
		var out []string
		for _, r := range matchRoles(in, roles) {
			out = append(out, r.Name)
		}
		slices.Sort(out)
		return out
	}
	cases := map[string][]string{
		"hi @dev":                  {"Dev"},
		"hi @DEV, ok":              {"Dev"},
		"@Dev Team standup":        {"Dev Team"},
		"@dev team and @qa.":       {"Dev Team", "QA"},
		"@dev and @Dev Team":       {"Dev", "Dev Team"},
		"@developer is a user":     nil,
		"mail@dev.example":         nil,
		"x@qa":                     nil,
		"(@qa)":                    {"QA"},
		"@dev.ops is something":    nil,
		"no mention here":          nil,
		"@dev-ops and @qa_lead":    nil,
		"ping @QA- thanks":         {"QA"},
		"@dev.- ok":                {"Dev"},
		"@dev-.ops":                nil,
		"first @qa then more text": {"QA"},
	}
	for in, want := range cases {
		if got := names(in); !slices.Equal(got, want) {
			t.Errorf("matchRoles(%q) = %v, want %v", in, got, want)
		}
	}
	if got := matchRoles("@dev", nil); got != nil {
		t.Errorf("no roles: %v", got)
	}
}

func TestParseMentions(t *testing.T) {
	cases := map[string][]string{
		"hey @Cool_Guy, schau":        {"cool_guy"},
		"@all and @here!":             {"all", "here"},
		"satzende @max.":              {"max"},
		"(@max) @max @MAX":            {"max"},
		"mail@max.de":                 nil,
		"@a is too short":             nil,
		"`@max` counts anyway":        {"max"},
		"a.b@max and x-@moritz":       nil,
		"@cool_guy.dev is a username": {"cool_guy.dev"},
	}
	for in, want := range cases {
		if got := ParseMentions(in); !slices.Equal(got, want) {
			t.Errorf("ParseMentions(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestPhraseRoleIsMaskedBeforeUserNames(t *testing.T) {
	team := MentionRole{ID: uuid.New(), Name: "Dev Team"}
	hits := roleHits("@Dev Team and @bob", []MentionRole{team})
	if got := ParseMentions(maskPhraseRoles("@Dev Team and @bob", hits)); !slices.Equal(got, []string{"bob"}) {
		t.Errorf("user names after masking = %v, want only bob", got)
	}
}

func TestMentionSkippedUnderNoUsers(t *testing.T) {
	bob := uuid.New()
	policy := MentionPolicy{NoUsers: true}
	cases := []struct {
		name    string
		policy  MentionPolicy
		grouped bool
		want    bool
	}{
		{"named user", policy, false, true},
		{"allowed user", MentionPolicy{NoUsers: true, AllowUsers: []uuid.UUID{bob}}, false, false},
		{"reached by @all or @here as well", policy, true, false},
		{"no policy", MentionPolicy{}, false, false},
	}
	for _, c := range cases {
		if got := mentionSkipped(c.policy, bob, c.grouped); got != c.want {
			t.Errorf("%s: skipped = %v, want %v", c.name, got, c.want)
		}
	}
}

// Under NoUsers a name next to @all is plain text: the member stays, reached
// by the group, unless AllowUsers lists them. A bot, which @all never
// reaches, is dropped.
func TestAllowedMentionsDemoteAPlainTextName(t *testing.T) {
	bob, eve, bot := uuid.New(), uuid.New(), uuid.New()
	ids := map[uuid.UUID]mentionHit{
		bob: {ID: bob, Kind: core.MentionKindName, Grouped: true},
		eve: {ID: eve, Kind: core.MentionKindName, Grouped: true},
		bot: {ID: bot, Kind: core.MentionKindName},
	}
	kinds := func(policy MentionPolicy) map[uuid.UUID]int16 {
		out := map[uuid.UUID]int16{}
		for _, u := range allowedMentions(ids, policy) {
			out[u.ID] = u.Kind
		}
		return out
	}
	if got := kinds(MentionPolicy{}); len(got) != 3 || got[bob] != core.MentionKindName || got[bot] != core.MentionKindName {
		t.Errorf("names allowed: %v", got)
	}
	got := kinds(MentionPolicy{NoUsers: true, AllowUsers: []uuid.UUID{eve}})
	if got[bob] != core.MentionKindGroup || got[eve] != core.MentionKindName {
		t.Errorf("NoUsers with eve listed: bob %d (want group), eve %d (want name)", got[bob], got[eve])
	}
	if _, ok := got[bot]; ok {
		t.Errorf("NoUsers: the bot named next to @all is still mentioned: %v", got)
	}
}
