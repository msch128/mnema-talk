package chat

import (
	"slices"
	"testing"
)

func TestParseMentions(t *testing.T) {
	cases := map[string][]string{
		"hey @Herzog, schau":           {"herzog"},
		"@all und @here!":              {"all", "here"},
		"satzende @max.":               {"max"},
		"(@max) @max @MAX":             {"max"},
		"mail@max.de":                  nil,
		"@ab ist zu kurz":              nil,
		"`@max` zählt trotzdem":        {"max"},
		"a.b@max und x-@moritz":        nil,
		"@herzog.dev ist ein Username": {"herzog.dev"},
	}
	for in, want := range cases {
		if got := ParseMentions(in); !slices.Equal(got, want) {
			t.Errorf("ParseMentions(%q) = %v, want %v", in, got, want)
		}
	}
}
