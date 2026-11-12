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
	bob, eve := uuid.New(), uuid.New()
	online := []uuid.UUID{eve}
	policy := MentionPolicy{NoUsers: true}
	cases := []struct {
		name    string
		policy  MentionPolicy
		targets mentionTargets
		id      uuid.UUID
		want    bool
	}{
		{"named user", policy, mentionTargets{usernames: []string{"bob"}}, bob, true},
		{"allowed user", MentionPolicy{NoUsers: true, AllowUsers: []uuid.UUID{bob}}, mentionTargets{usernames: []string{"bob"}}, bob, false},
		{"@all reaches everyone", policy, mentionTargets{all: true}, bob, false},
		{"@here reaches the online", policy, mentionTargets{here: true}, eve, false},
		{"@here does not make an offline name a mention", policy, mentionTargets{here: true, usernames: []string{"bob"}}, bob, true},
		{"no policy", MentionPolicy{}, mentionTargets{usernames: []string{"bob"}}, bob, false},
	}
	for _, c := range cases {
		if got := mentionSkipped(c.policy, c.targets, online, c.id); got != c.want {
			t.Errorf("%s: skipped = %v, want %v", c.name, got, c.want)
		}
	}
}

// Under NoUsers a name next to @all is plain text: the member stays, reached
// by the group, unless AllowUsers lists them.
func TestAllowedMentionsDemoteAPlainTextName(t *testing.T) {
	bob, eve := uuid.New(), uuid.New()
	ids := map[uuid.UUID]int16{bob: core.MentionKindName, eve: core.MentionKindName}
	all := mentionTargets{all: true, usernames: []string{"bob", "eve"}}
	kinds := func(policy MentionPolicy) map[uuid.UUID]int16 {
		out := map[uuid.UUID]int16{}
		for _, u := range allowedMentions(ids, policy, all, nil) {
			out[u.ID] = u.Kind
		}
		return out
	}
	if got := kinds(MentionPolicy{}); got[bob] != core.MentionKindName || got[eve] != core.MentionKindName {
		t.Errorf("names allowed: %v", got)
	}
	got := kinds(MentionPolicy{NoUsers: true, AllowUsers: []uuid.UUID{eve}})
	if got[bob] != core.MentionKindGroup || got[eve] != core.MentionKindName {
		t.Errorf("NoUsers with eve listed: bob %d (want group), eve %d (want name)", got[bob], got[eve])
	}
}
