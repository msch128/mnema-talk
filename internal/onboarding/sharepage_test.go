package onboarding

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestPlainSnippetIsOneShortLineOfPlainText(t *testing.T) {
	md := "# Welcome\n\n**Hello** and [welcome](https://example.org) to the *team*.\n\n- first\n- second"
	if got := plainSnippet(md, 200); got != "Welcome Hello and welcome to the team. first second" {
		t.Errorf("snippet = %q", got)
	}
	long := plainSnippet(strings.Repeat("alpha beta gamma ", 40), 60)
	if utf8.RuneCountInString(long) > 60 || !strings.HasSuffix(long, "…") || strings.Contains(long, "  ") {
		t.Errorf("long snippet = %q (%d runes)", long, utf8.RuneCountInString(long))
	}
	if strings.HasSuffix(strings.TrimSuffix(long, "…"), " ") || strings.Contains(long, "gamm…") && !strings.Contains(long, "gamma") {
		t.Errorf("cut should fall on a word: %q", long)
	}
	if got := plainSnippet("   ", 50); got != "" {
		t.Errorf("blank text = %q", got)
	}
	if got := plainSnippet("||secret|| text", 50); strings.Contains(got, "secret") {
		t.Errorf("a spoiler must not reach a public page: %q", got)
	}
}
