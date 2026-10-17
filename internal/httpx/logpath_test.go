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
	}
	for in, want := range cases {
		if got := LogPath(in); got != want {
			t.Errorf("LogPath(%q) = %q, want %q", in, got, want)
		}
	}
}
