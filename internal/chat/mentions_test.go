package chat

import (
	"slices"
	"testing"

	"github.com/google/uuid"
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
		"hey @Herzog, schau":        {"herzog"},
		"@all and @here!":           {"all", "here"},
		"satzende @max.":            {"max"},
		"(@max) @max @MAX":          {"max"},
		"mail@max.de":               nil,
		"@ab is too short":          nil,
		"`@max` counts anyway":      {"max"},
		"a.b@max and x-@moritz":     nil,
		"@herzog.dev is a username": {"herzog.dev"},
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
