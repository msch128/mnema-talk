package chat

import (
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
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

// A tag deleted between validation and the insert reads like a tag of another
// channel, not like a server fault.
func TestPostTagErrorMapsAVanishedTag(t *testing.T) {
	err := postTagError(&pgconn.PgError{Code: "23503"})
	if !errors.Is(err, errTagWrongChannel) {
		t.Fatalf("foreign key violation: %v", err)
	}
	other := errors.New("boom")
	if got := postTagError(other); errors.Is(got, errTagWrongChannel) || !errors.Is(got, other) {
		t.Fatalf("other failure: %v", got)
	}
}

func TestCleanTagEmojiCountsCharacters(t *testing.T) {
	// 8 four-byte emoji: 32 bytes, so the limit counts characters, not bytes.
	many := strings.Repeat("\U0001F41B", MaxForumTagEmojiLen)
	if got, err := CleanTagEmoji(many); err != nil || got != many {
		t.Fatalf("emoji at the character limit rejected: %v", err)
	}
	if _, err := CleanTagEmoji(many + "\U0001F41B"); err == nil {
		t.Fatal("emoji beyond the character limit accepted")
	}
}

func TestForumSortModes(t *testing.T) {
	for mode, newest := range map[string]bool{"": false, "latest_activity": false, "activity": false, "newest": true, "creation": true} {
		if got, err := forumSortNewest(mode); err != nil || got != newest {
			t.Errorf("sort %q = %v, %v; want %v", mode, got, err, newest)
		}
	}
	if _, err := forumSortNewest("oldest"); err == nil {
		t.Fatal("unknown sort accepted")
	}
}
