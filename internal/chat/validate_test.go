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
	if _, err := ValidateContent(strings.Repeat("\u00e4", MaxMessageLen), false); err != nil {
		t.Errorf("limit must count characters: %v", err)
	}
}

func TestValidateEmoji(t *testing.T) {
	for _, ok := range []string{"👍", "🔥", "😂", ":custom:", "👨\u200d👩\u200d👧", "❤\ufe0f", "🏴\U000e0067\U000e0062\U000e0065\U000e006e\U000e0067\U000e007f"} {
		if _, err := ValidateEmoji(ok); err != nil {
			t.Errorf("%q rejected: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "a b", "<img>", "\x07", strings.Repeat("😂", 20),
		"\u200bx", "a\u202eb", "\u2066x", "\ufeffx", "\u00adx"} {
		if _, err := ValidateEmoji(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}
