package chat

import (
	"strings"
	"testing"
)

func TestValidateContent(t *testing.T) {
	if _, err := ValidateContent("   ", false); err == nil {
		t.Error("blank message accepted")
	}
	if got, err := ValidateContent("  ", true); err != nil || got != "" {
		t.Errorf("blank caption with attachment: %q %v", got, err)
	}
	if _, err := ValidateContent(strings.Repeat("x", MaxMessageLen+1), false); err == nil {
		t.Error("over-long message accepted")
	}
	if _, err := ValidateContent(strings.Repeat("ä", MaxMessageLen), false); err != nil {
		t.Errorf("limit must count characters: %v", err)
	}
}

func TestValidateEmoji(t *testing.T) {
	for _, ok := range []string{"👍", "🔥", "😂", ":custom:"} {
		if _, err := ValidateEmoji(ok); err != nil {
			t.Errorf("%q rejected: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "a b", "<img>", "\x07", strings.Repeat("😂", 20)} {
		if _, err := ValidateEmoji(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}
