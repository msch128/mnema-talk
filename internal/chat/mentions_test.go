package chat

import (
	"slices"
	"testing"

	"github.com/google/uuid"
)

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
