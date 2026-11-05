package auth

import (
	"context"
	"strings"
	"testing"
)

// Every mail to the same member comes out in one language for the same inputs.
func TestAccountMailsShareOneLanguage(t *testing.T) {
	h := &Handler{PublicURL: "https://talk.example.org"}
	for _, tc := range []struct {
		stored, header, marker string
	}{
		{"de", "en", "Hallo"},
		{"en", "de", "Hello"},
		{"", "de-DE", "Hallo"},
		{"", "", "Hello"},
	} {
		loc := mailLocale(tc.stored, tc.header)
		for name, body := range map[string]string{
			"verify": h.verifyMail(context.Background(), "a@example.org", loc, "mia", "tok").Body,
			"reset":  h.resetMail(context.Background(), "a@example.org", loc, "mia", "tok").Body,
			"alert":  h.alertMail(context.Background(), "a@example.org", loc, "mia").Body,
		} {
			if !strings.HasPrefix(body, tc.marker) {
				t.Errorf("%s mail for stored=%q header=%q starts %q, want %q", name, tc.stored, tc.header, body[:8], tc.marker)
			}
		}
	}
}
