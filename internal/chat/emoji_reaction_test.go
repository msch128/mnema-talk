package chat

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/emojitext"
)

func TestValidateEmojiAcceptsRealEmojiSequences(t *testing.T) {
	cases := map[string]string{
		"party popper":          "🎉",
		"partying face":         "🥳",
		"thumb with skin tone":  "👍🏽",
		"family":                "👨‍👩‍👧",
		"family of four":        "👨‍👩‍👧‍👦",
		"rainbow flag":          "🏳️‍🌈",
		"country flag":          "🇩🇪",
		"keycap":                "1️⃣",
		"keycap hash":           "#️⃣",
		"England flag":          "🏴󠁧󠁢󠁥󠁮󠁧󠁿",
		"kiss with skin tones":  "🧑🏽‍❤️‍💋‍🧑🏿",
		"couple with heart":     "👩🏽‍❤️‍👨🏿",
		"handshake tones":       "🧑🏻‍🤝‍🧑🏿",
		"family with skin tone": "🧑🏿‍🧑🏿‍🧒🏿‍🧒🏿",
		"running right":         "🏃🏽‍♀️‍➡️",
		"eye in speech bubble":  "👁️‍🗨️",
		"newer emoji":           "🫩",
		"newer emoji two":       "🫆",
		"heart text style":      "❤",
		"heart emoji style":     "❤️",
	}
	for name, emoji := range cases {
		t.Run(name, func(t *testing.T) {
			if got, err := ValidateEmoji(emoji); err != nil || got != emoji {
				t.Fatalf("ValidateEmoji(%q) = %q, %v", emoji, got, err)
			}
		})
	}
}

func TestValidateEmojiAcceptsCustomToken(t *testing.T) {
	token := emojitext.Token("party", uuid.New())
	if got, err := ValidateEmoji(token); err != nil || got != token {
		t.Fatalf("ValidateEmoji(%q) = %q, %v", token, got, err)
	}
}

func TestValidateEmojiRefusesWhatIsNoEmoji(t *testing.T) {
	cases := map[string]string{
		"empty":        "",
		"space":        "👍 👍",
		"newline":      "👍\n👍",
		"bidi":         "👍‮",
		"zero width":   "👍​",
		"word joiner":  "⁠👍",
		"angle":        "<b>",
		"too long":     strings.Repeat("👍", 17),
		"far too long": strings.Repeat("a", 400),
		"bad utf8":     "\xff\xfe",
	}
	for name, emoji := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ValidateEmoji(emoji); err == nil {
				t.Fatalf("ValidateEmoji(%q) accepted", emoji)
			}
		})
	}
}
