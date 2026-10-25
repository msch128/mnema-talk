package chat

import (
	"strings"
	"testing"
)

func TestSlugify(t *testing.T) {
	uuidName := "123e4567-e89b-12d3-a456-426614174000"
	for name, want := range map[string]string{
		"General": "general",
		"Gr\u00f6\u00dfe & \u00c4rger \u00fcber \u00d6l": "groesse-aerger-ueber-oel",
		"Café Crème":                      "cafe-creme",
		"Łódź Ørsted":                     "lodz-orsted",
		"  Game --- Night 🎮 ":             "game-night",
		"a__b..c":                         "a-b-c",
		"🎮🎮":                              "channel",
		"":                                "channel",
		"---":                             "channel",
		"日本語":                             "channel",
		"Talk 1":                          "talk-1",
		uuidName:                          "channel-" + uuidName,
		strings.Repeat("a", 59) + " bbbb": strings.Repeat("a", 59),
		strings.Repeat("x", 100):          strings.Repeat("x", MaxSlugLength),
	} {
		if got := Slugify(name); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestSlugCandidateStaysWithinTheLimit(t *testing.T) {
	long := strings.Repeat("x", MaxSlugLength)
	for n, want := range map[int]string{1: long, 2: long[:MaxSlugLength-2] + "-2", 10: long[:MaxSlugLength-3] + "-10"} {
		got := slugCandidate(long, n)
		if got != want || len(got) > MaxSlugLength {
			t.Errorf("slugCandidate(long, %d) = %q, want %q", n, got, want)
		}
	}
	if got := slugCandidate("general", 3); got != "general-3" {
		t.Errorf("got %q", got)
	}
	// A cut that ends in a dash drops it before the suffix.
	if got := slugCandidate(strings.Repeat("a", 57)+"-bb", 2); got != strings.Repeat("a", 57)+"-2" {
		t.Errorf("got %q", got)
	}
}

func TestKeepsSlug(t *testing.T) {
	for _, c := range []struct {
		current, base string
		want          bool
	}{
		{"general", "general", true},
		{"general-2", "general", true},
		{"general-x", "general", false},
		{"general", "general-2", false},
		{"lobby", "general", false},
		{"general-2-2", "general", false},
	} {
		if got := keepsSlug(c.current, c.base); got != c.want {
			t.Errorf("keepsSlug(%q, %q) = %v, want %v", c.current, c.base, got, c.want)
		}
	}
}
