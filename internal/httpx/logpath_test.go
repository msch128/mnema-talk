package httpx

import "testing"

func TestLogPathRedactsWebhookTokens(t *testing.T) {
	cases := map[string]string{
		"/api/webhooks/abc/secret-token": "/api/webhooks/abc/[redacted]",
		"/api/webhooks/abc/secret/extra": "/api/webhooks/abc/[redacted]",
		"/api/webhooks/abc":              "/api/webhooks/abc",
		"/api/admin/webhooks/abc/token":  "/api/admin/webhooks/abc/token",
		"/api/channels/1/messages":       "/api/channels/1/messages",
		"/":                              "/",
		// a bot's interaction token is as good as the bot's say over that interaction
		"/api/bot/v1/interactions/7b0a/tok-en_1/callback":  "/api/bot/v1/interactions/7b0a/[redacted]/callback",
		"/api/bot/v1/interactions/7b0a/tok-en_1/followups": "/api/bot/v1/interactions/7b0a/[redacted]/followups",
		"/api/bot/v1/interactions/7b0a/tok-en_1":           "/api/bot/v1/interactions/7b0a/[redacted]",
		"/api/bot/v1/interactions/7b0a":                    "/api/bot/v1/interactions/7b0a",
		"/api/bot/v1/commands":                             "/api/bot/v1/commands",
	}
	for in, want := range cases {
		if got := LogPath(in); got != want {
			t.Errorf("LogPath(%q) = %q, want %q", in, got, want)
		}
	}
}
