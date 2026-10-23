package emojitext

import (
	"testing"

	"github.com/google/uuid"
)

func TestTokenRoundTripsAndDegradesToText(t *testing.T) {
	id := uuid.New()
	tok := Token("party_parrot", id)
	if !IsToken(tok) || len(tok) > MaxTokenLen {
		t.Fatalf("token %q", tok)
	}
	if got := Plain("hi " + tok + " and " + tok + "!"); got != "hi :party_parrot: and :party_parrot:!" {
		t.Fatalf("plain: %q", got)
	}
}

func TestMalformedTokensAreNotTokens(t *testing.T) {
	id := uuid.New().String()
	for _, s := range []string{"", ":a:", "<:A:" + id + ">", "<:x:" + id + ">", "<:ok:not-a-uuid>", "<:ok:" + id + "> ", "x<:ok:" + id + ">"} {
		if IsToken(s) {
			t.Errorf("%q accepted", s)
		}
	}
	if Plain("<:ok:nope>") != "<:ok:nope>" || !ValidName("a_1") || ValidName("A1") || ValidName("a") {
		t.Fatal("name or plain rules")
	}
}

func TestIDReadsOnlyAWholeToken(t *testing.T) {
	id := uuid.New()
	if got, ok := ID(Token("party", id)); !ok || got != id {
		t.Errorf("ID(token) = %v, %v", got, ok)
	}
	for _, s := range []string{"👍", "", "x" + Token("party", id), Token("party", id) + "x", "<:a:not-a-uuid>"} {
		if _, ok := ID(s); ok {
			t.Errorf("ID(%q) accepted", s)
		}
	}
}
