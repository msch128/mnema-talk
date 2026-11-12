package core

import (
	"encoding/json"
	"os"
	"testing"
)

// The web app sends and shows reactions in the same form
// (web/src/shared/lib/reactionForm.ts). Its test writes
// testdata/reaction_forms.json (REACTION_FORMS_WRITE=1) from every emoji of the
// picker and a set of hand-made forms; this test checks the server's rule
// against the same file without regenerating it.
func TestReactionFormsMatchTheVectors(t *testing.T) {
	raw, err := os.ReadFile("testdata/reaction_forms.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors []struct{ Emoji, Form string }
	if err := json.Unmarshal(raw, &vectors); err != nil {
		t.Fatal(err)
	}
	if len(vectors) < 3000 {
		t.Fatalf("only %d vectors", len(vectors))
	}
	for _, v := range vectors {
		if got := ReactionForm(v.Emoji); got != v.Form {
			t.Errorf("ReactionForm(%+q) = %+q, the web app says %+q", v.Emoji, got, v.Form)
		}
		if got := ReactionForm(ReactionKey(v.Emoji)); got != v.Form {
			t.Errorf("ReactionForm(ReactionKey(%+q)) = %+q, want %+q: one emoji, two reactions", v.Emoji, got, v.Form)
		}
	}
}

func TestReactionFormMergesOnlyTheFormsOfOneEmoji(t *testing.T) {
	same := [][2]string{
		{"\u2764", "\u2764\uFE0F"},
		{"\U0001F44D", "\U0001F44D\uFE0F"},
		{"\u2764\u200D\U0001F525", "\u2764\uFE0F\u200D\U0001F525"},
		{"1\u20E3", "1\uFE0F\u20E3"},
	}
	for _, p := range same {
		if ReactionForm(p[0]) != ReactionForm(p[1]) || ReactionKey(p[0]) != ReactionKey(p[1]) {
			t.Errorf("%+q and %+q are one emoji: forms %+q, %+q", p[0], p[1], ReactionForm(p[0]), ReactionForm(p[1]))
		}
	}
	apart := [][2]string{
		{"\U0001F44D", "\U0001F44D\U0001F3FD"},                 // skin tone
		{"\u2764\uFE0F", "\u2764\uFE0F\u200D\U0001F525"},       // ZWJ sequence
		{"\U0001F468\u200D\U0001F469", "\U0001F468\U0001F469"}, // joined or not
		{"1", "1\u20E3"},
	}
	for _, p := range apart {
		if ReactionKey(p[0]) == ReactionKey(p[1]) {
			t.Errorf("%+q and %+q are different emoji but share a key", p[0], p[1])
		}
	}
	token := "<:party:11111111-2222-3333-4444-555555555555>"
	if ReactionForm(token) != token {
		t.Errorf("a custom emoji token changed: %+q", ReactionForm(token))
	}
}
