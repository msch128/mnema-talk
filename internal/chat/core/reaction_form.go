package core

import (
	"strings"
	"unicode"

	"github.com/msch128/mnema-talk/internal/emojitext"
)

// One emoji can be written with or without the variation selector U+FE0F: the
// quick reactions send a thumbs up as U+1F44D, the full picker as U+1F44D
// U+FE0F, and a red heart typed or sent by a bot may lack it. A reaction is
// the same reaction in every such form. The web app applies the same rule
// (web/src/shared/lib/reactionEmoji.ts, reactionForm); both are checked against
// testdata/reaction_forms.json, which the web test writes from every emoji of
// the picker.
const (
	emojiSelector = "\uFE0F" // VS16: shown as an emoji
	textSelector  = '\uFE0E' // VS15: shown as text
	keycapMark    = '\u20E3'
)

// ReactionKey is what makes two reactions one: the emoji without U+FE0F. Skin
// tones, joiners and every other character stay, so emoji that differ never
// share a key. The reaction queries compute the same key in SQL
// (reactionKeySQL in internal/chat/messages).
func ReactionKey(emoji string) string {
	return strings.ReplaceAll(emoji, emojiSelector, "")
}

// ReactionForm is the one form a reaction is stored, sent and shown in: the
// fully-qualified emoji of Unicode (UTS #51). U+FE0F follows exactly the
// characters that are shown as text by default (textDefaultEmoji), unless a
// skin tone or U+FE0E follows them; a digit, # and * take it only in a keycap.
// An emoji that is shown as an emoji by default goes without (the picker's
// redundant U+FE0F after a lone one is dropped). A custom emoji token stays as
// it is; other text only loses U+FE0F.
func ReactionForm(emoji string) string {
	if emojitext.IsToken(emoji) {
		return emoji
	}
	runes := []rune(ReactionKey(emoji))
	var b strings.Builder
	for i, r := range runes {
		b.WriteRune(r)
		next := rune(0)
		if i+1 < len(runes) {
			next = runes[i+1]
		}
		if takesEmojiSelector(r, next) {
			b.WriteString(emojiSelector)
		}
	}
	return b.String()
}

func takesEmojiSelector(r, next rune) bool {
	if next == textSelector || (next >= 0x1F3FB && next <= 0x1F3FF) {
		return false
	}
	if r == '#' || r == '*' || (r >= '0' && r <= '9') {
		return next == keycapMark
	}
	return unicode.Is(textDefaultEmoji, r)
}

// textDefaultEmoji are the emoji characters (Emoji=Yes) without
// Emoji_Presentation, besides the keycap bases: Unicode 16.0, emoji-data.txt.
// The web app holds the same table.
var textDefaultEmoji = &unicode.RangeTable{
	R16: []unicode.Range16{
		{0x00A9, 0x00A9, 1}, {0x00AE, 0x00AE, 1}, {0x203C, 0x203C, 1}, {0x2049, 0x2049, 1},
		{0x2122, 0x2122, 1}, {0x2139, 0x2139, 1}, {0x2194, 0x2199, 1}, {0x21A9, 0x21AA, 1},
		{0x2328, 0x2328, 1}, {0x23CF, 0x23CF, 1}, {0x23ED, 0x23EF, 1}, {0x23F1, 0x23F2, 1},
		{0x23F8, 0x23FA, 1}, {0x24C2, 0x24C2, 1}, {0x25AA, 0x25AB, 1}, {0x25B6, 0x25B6, 1},
		{0x25C0, 0x25C0, 1}, {0x25FB, 0x25FC, 1}, {0x2600, 0x2604, 1}, {0x260E, 0x260E, 1},
		{0x2611, 0x2611, 1}, {0x2618, 0x2618, 1}, {0x261D, 0x261D, 1}, {0x2620, 0x2620, 1},
		{0x2622, 0x2623, 1}, {0x2626, 0x2626, 1}, {0x262A, 0x262A, 1}, {0x262E, 0x262F, 1},
		{0x2638, 0x263A, 1}, {0x2640, 0x2640, 1}, {0x2642, 0x2642, 1}, {0x265F, 0x2660, 1},
		{0x2663, 0x2663, 1}, {0x2665, 0x2666, 1}, {0x2668, 0x2668, 1}, {0x267B, 0x267B, 1},
		{0x267E, 0x267E, 1}, {0x2692, 0x2692, 1}, {0x2694, 0x2697, 1}, {0x2699, 0x2699, 1},
		{0x269B, 0x269C, 1}, {0x26A0, 0x26A0, 1}, {0x26A7, 0x26A7, 1}, {0x26B0, 0x26B1, 1},
		{0x26C8, 0x26C8, 1}, {0x26CF, 0x26CF, 1}, {0x26D1, 0x26D1, 1}, {0x26D3, 0x26D3, 1},
		{0x26E9, 0x26E9, 1}, {0x26F0, 0x26F1, 1}, {0x26F4, 0x26F4, 1}, {0x26F7, 0x26F9, 1},
		{0x2702, 0x2702, 1}, {0x2708, 0x2709, 1}, {0x270C, 0x270D, 1}, {0x270F, 0x270F, 1},
		{0x2712, 0x2712, 1}, {0x2714, 0x2714, 1}, {0x2716, 0x2716, 1}, {0x271D, 0x271D, 1},
		{0x2721, 0x2721, 1}, {0x2733, 0x2734, 1}, {0x2744, 0x2744, 1}, {0x2747, 0x2747, 1},
		{0x2763, 0x2764, 1}, {0x27A1, 0x27A1, 1}, {0x2934, 0x2935, 1}, {0x2B05, 0x2B07, 1},
		{0x3030, 0x3030, 1}, {0x303D, 0x303D, 1}, {0x3297, 0x3297, 1}, {0x3299, 0x3299, 1},
	},
	R32: []unicode.Range32{
		{0x1F170, 0x1F171, 1}, {0x1F17E, 0x1F17F, 1}, {0x1F202, 0x1F202, 1}, {0x1F237, 0x1F237, 1},
		{0x1F321, 0x1F321, 1}, {0x1F324, 0x1F32C, 1}, {0x1F336, 0x1F336, 1}, {0x1F37D, 0x1F37D, 1},
		{0x1F396, 0x1F397, 1}, {0x1F399, 0x1F39B, 1}, {0x1F39E, 0x1F39F, 1}, {0x1F3CB, 0x1F3CE, 1},
		{0x1F3D4, 0x1F3DF, 1}, {0x1F3F3, 0x1F3F3, 1}, {0x1F3F5, 0x1F3F5, 1}, {0x1F3F7, 0x1F3F7, 1},
		{0x1F43F, 0x1F43F, 1}, {0x1F441, 0x1F441, 1}, {0x1F4FD, 0x1F4FD, 1}, {0x1F549, 0x1F54A, 1},
		{0x1F56F, 0x1F570, 1}, {0x1F573, 0x1F579, 1}, {0x1F587, 0x1F587, 1}, {0x1F58A, 0x1F58D, 1},
		{0x1F590, 0x1F590, 1}, {0x1F5A5, 0x1F5A5, 1}, {0x1F5A8, 0x1F5A8, 1}, {0x1F5B1, 0x1F5B2, 1},
		{0x1F5BC, 0x1F5BC, 1}, {0x1F5C2, 0x1F5C4, 1}, {0x1F5D1, 0x1F5D3, 1}, {0x1F5DC, 0x1F5DE, 1},
		{0x1F5E1, 0x1F5E1, 1}, {0x1F5E3, 0x1F5E3, 1}, {0x1F5E8, 0x1F5E8, 1}, {0x1F5EF, 0x1F5EF, 1},
		{0x1F5F3, 0x1F5F3, 1}, {0x1F5FA, 0x1F5FA, 1}, {0x1F6CB, 0x1F6CB, 1}, {0x1F6CD, 0x1F6CF, 1},
		{0x1F6E0, 0x1F6E5, 1}, {0x1F6E9, 0x1F6E9, 1}, {0x1F6F0, 0x1F6F0, 1}, {0x1F6F3, 0x1F6F3, 1},
	},
	LatinOffset: 2,
}
