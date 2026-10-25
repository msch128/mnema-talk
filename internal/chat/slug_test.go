package chat

import (
	"strings"
	"testing"

	"github.com/google/uuid"
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

func TestSlugCandidateSuffixComesFromTheChannelsOwnID(t *testing.T) {
	id := uuid.MustParse("0f3a9c1e-1111-4222-8333-444455556666")
	long := strings.Repeat("x", MaxSlugLength)
	for n, want := range map[int]string{0: long, 1: long[:MaxSlugLength-5] + "-0f3a", 2: long[:MaxSlugLength-7] + "-0f3a9c", 3: long[:MaxSlugLength-9] + "-0f3a9c1e"} {
		got := slugCandidate(long, id, n)
		if got != want || len(got) > MaxSlugLength {
			t.Errorf("slugCandidate(long, %d) = %q, want %q", n, got, want)
		}
	}
	if got := slugCandidate("general", id, 1); got != "general-0f3a" {
		t.Errorf("got %q", got)
	}
	// A cut that ends in a dash drops it before the suffix.
	if got := slugCandidate(strings.Repeat("a", 54)+"-bb", id, 1); got != strings.Repeat("a", 54)+"-0f3a" {
		t.Errorf("got %q", got)
	}
}

func TestKeepsSlug(t *testing.T) {
	id := uuid.MustParse("0f3a9c1e-1111-4222-8333-444455556666")
	for _, c := range []struct {
		current, base string
		want          bool
	}{
		{"general", "general", true},
		{"general-0f3a", "general", true},
		{"general-0f3a9c", "general", true},
		{"general-2", "general", false},
		{"general-beef", "general", false},
		{"general", "general-0f3a", false},
		{"lobby", "general", false},
	} {
		if got := keepsSlug(c.current, c.base, id); got != c.want {
			t.Errorf("keepsSlug(%q, %q) = %v, want %v", c.current, c.base, got, c.want)
		}
	}
}

func TestTempTalkSlugIsFixedAndReserved(t *testing.T) {
	id := uuid.MustParse("0f3a9c1e-1111-4222-8333-444455556666")
	if got := TempTalkSlug(id); got != "talk-0f3a9c1e" || !tempSlugPattern.MatchString(got) {
		t.Fatalf("slug %q", got)
	}
	if tempSlugPattern.MatchString("talk-1") || tempSlugPattern.MatchString("talk-0f3a9c1") {
		t.Fatal("pattern too wide")
	}
}
