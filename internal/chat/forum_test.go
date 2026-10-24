package chat

import (
	"strings"
	"testing"
)

func TestCleanPostTitle(t *testing.T) {
	valid, err := CleanPostTitle("  Welcome to the forum!  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if valid != "Welcome to the forum!" {
		t.Errorf("expected trimmed title, got %q", valid)
	}

	for _, empty := range []string{"", "   ", "\t\n"} {
		if _, err := CleanPostTitle(empty); err == nil {
			t.Errorf("expected error for empty title %q", empty)
		}
	}

	overlong := strings.Repeat("a", MaxPostTitleLen+1)
	if _, err := CleanPostTitle(overlong); err == nil {
		t.Error("expected error for overlong title")
	}

	exact := strings.Repeat("\u00e4", MaxPostTitleLen)
	if got, err := CleanPostTitle(exact); err != nil || got != exact {
		t.Errorf("expected exact unicode title accepted: %v", err)
	}
}

func TestCleanTagName(t *testing.T) {
	valid, err := CleanTagName("  help-needed  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if valid != "help-needed" {
		t.Errorf("expected trimmed name, got %q", valid)
	}

	for _, empty := range []string{"", "   "} {
		if _, err := CleanTagName(empty); err == nil {
			t.Errorf("expected error for empty tag name %q", empty)
		}
	}

	overlong := strings.Repeat("x", MaxForumTagNameLen+1)
	if _, err := CleanTagName(overlong); err == nil {
		t.Error("expected error for overlong tag name")
	}

	exact := strings.Repeat("\u00fc", MaxForumTagNameLen)
	if got, err := CleanTagName(exact); err != nil || got != exact {
		t.Errorf("expected exact tag name accepted: %v", err)
	}
}

func TestCleanTagEmoji(t *testing.T) {
	empty, err := CleanTagEmoji("")
	if err != nil || empty != "" {
		t.Errorf("expected empty emoji accepted: %v", err)
	}

	valid, err := CleanTagEmoji("🚀")
	if err != nil || valid != "🚀" {
		t.Errorf("expected emoji accepted: %v", err)
	}

	overlong := strings.Repeat("a", MaxForumTagEmojiLen+1)
	if _, err := CleanTagEmoji(overlong); err == nil {
		t.Error("expected error for overlong emoji")
	}
}
