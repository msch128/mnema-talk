package wiki

import (
	"strings"
	"testing"
)

func TestDeriveSlug(t *testing.T) {
	for title, want := range map[string]string{
		"Raid guide":                  "raid-guide",
		"  Größe & Ärger!  ":          "groesse-aerger",
		"Café – Menü (v2)":            "cafe-menue-v2",
		"C++ / Go":                    "c-go",
		"日本語":                         "page",
		"":                            "page",
		"---":                         "page",
		"Ünder_score and--dashes":     "uender-score-and-dashes",
		strings.Repeat("abc ", 40):    strings.TrimRight(strings.Repeat("abc-", 40)[:MaxSlugLen], "-"),
		"Already-a-slug-123":          "already-a-slug-123",
		"naïve façade Łódź Šibenik ž": "naive-facade-lodz-sibenik-z",
	} {
		got := DeriveSlug(title)
		if got != want {
			t.Errorf("DeriveSlug(%q) = %q, want %q", title, got, want)
		}
		if got != "" && !slugPattern.MatchString(got) {
			t.Errorf("%q is no valid slug", got)
		}
	}
}

func TestValidSlug(t *testing.T) {
	for _, ok := range []string{"a", "faq", "raid-guide-2", "2024"} {
		if !ValidSlug(ok) {
			t.Errorf("%q should be valid", ok)
		}
	}
	for _, bad := range []string{"", "Upper", "a b", "-a", "a-", "a--b", "a_b", "search", "new", "api", strings.Repeat("a", MaxSlugLen+1), "ä"} {
		if ValidSlug(bad) {
			t.Errorf("%q should be invalid", bad)
		}
	}
}

func TestNumberedKeepsTheAddressWithinItsLimit(t *testing.T) {
	long := strings.Repeat("a", MaxSlugLen)
	got := numbered(long, 12)
	if len(got) > MaxSlugLen || !strings.HasSuffix(got, "-12") || !ValidSlug(got) {
		t.Fatalf("%q", got)
	}
	if numbered("faq", 2) != "faq-2" {
		t.Fatal("short address")
	}
}

func TestSnippetShowsTheWordsAroundTheMatch(t *testing.T) {
	text := strings.Repeat("filler ", 60) + "the Needle sits here" + strings.Repeat(" more", 80)
	got := snippet(text, "needle")
	if !strings.Contains(got, "Needle") || !strings.HasPrefix(got, "…") || !strings.HasSuffix(got, "…") || len([]rune(got)) > snippetRunes+4 {
		t.Fatalf("snippet: %q", got)
	}
	if got := snippet("short text\n\nwith breaks", "zzz"); got != "short text with breaks" {
		t.Fatalf("no match shows the start: %q", got)
	}
	if got := snippet("", "x"); got != "" {
		t.Fatalf("empty: %q", got)
	}
	// Characters whose lower case has another length must not break it.
	if got := snippet("İİİ İstanbul", "stanbul"); got == "" {
		t.Fatal("special letters")
	}
}

func TestLikePatternEscapesWildcards(t *testing.T) {
	if got := likePattern(`50%_\x`); got != `%50\%\_\\x%` {
		t.Fatalf("%q", got)
	}
}

func TestCleanText(t *testing.T) {
	long := strings.Repeat("a", MaxContentBytes+1)
	if _, err := cleanText(nil, &long, ""); err != errTooLong {
		t.Fatalf("too long: %v", err)
	}
	for _, bad := range []string{"a\x00b", "\xff\xfe"} {
		if _, err := cleanText(nil, &bad, ""); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	title, content := "  Title  ", "  keeps\nspace  "
	got, err := cleanText(&title, &content, "  why ")
	if err != nil || got.title != "Title" || got.content != content || got.summary != "why" {
		t.Fatalf("%+v, %v", got, err)
	}
	if _, err := cleanText(nil, nil, strings.Repeat("s", MaxSummaryRunes+1)); err == nil {
		t.Fatal("a summary of 201 characters")
	}
}
