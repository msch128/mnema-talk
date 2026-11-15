package mention

import (
	"slices"
	"strings"
	"testing"
)

func TestNamesAreLowerCasedDistinctAndBounded(t *testing.T) {
	got := Names("hi @Alice, @alice. and mail a@bob.io @all @a " + "@" + strings.Repeat("a", MaxNameLen+1) + " @" + strings.Repeat("b", MaxNameLen))
	want := []string{"alice", "all", strings.Repeat("b", MaxNameLen)}
	if !slices.Equal(got, want) {
		t.Fatalf("Names = %v, want %v", got, want)
	}
}

func TestNamesMatchLegacyAndNewUsernames(t *testing.T) {
	got := Names("@Old-Timer.X, @a_b and @ab.")
	want := []string{"old-timer.x", "a_b", "ab"}
	if !slices.Equal(got, want) {
		t.Fatalf("Names = %v, want %v", got, want)
	}
}

func TestRewriteWritesLabelsForKnownNamesOnly(t *testing.T) {
	labels := map[string]string{"bot-abc": "Mnemo", "max": "Max M."}
	cases := map[string]string{
		"hi @bot-abc, see @Max.":     "hi @Mnemo, see @Max M..",
		"mail a@max.example @nobody": "mail a@max.example @nobody",
		"@all @max":                  "@all @Max M.",
		"@bot-abc-":                  "@Mnemo-",
	}
	for in, want := range cases {
		if got := Rewrite(in, labels); got != want {
			t.Errorf("Rewrite(%q) = %q, want %q", in, got, want)
		}
	}
	if got := Rewrite("@max", nil); got != "@max" {
		t.Errorf("no labels changed the text: %q", got)
	}
}
