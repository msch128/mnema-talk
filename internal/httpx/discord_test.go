package httpx

import (
	"testing"
)

func TestDiscordCodeExplicitWinsOverMessageWording(t *testing.T) {
	if got := discordCode(ErrNotFound("message thread not found")); got != DiscordCodeUnknownMessage {
		t.Fatalf("fallback = %d", got)
	}
	if got := discordCode(ErrNotFound("message thread not found").WithDiscordCode(DiscordCodeUnknownChannel)); got != DiscordCodeUnknownChannel {
		t.Fatalf("explicit code ignored: %d", got)
	}
	if got := discordCode(ErrInvalidInput("request body is empty").WithDiscordCode(50035)); got != 50035 {
		t.Fatalf("explicit code ignored: %d", got)
	}
}
