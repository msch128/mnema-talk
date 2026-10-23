package auth

import (
	"strings"
	"testing"
)

func TestMailLocaleRule(t *testing.T) {
	cases := []struct {
		name, stored, header, want string
	}{
		{"stored language wins over the request", "de", "en-US,en;q=0.9", "de"},
		{"stored english wins over a german request", "en", "de-DE,de;q=0.9", "en"},
		{"no stored language uses the request", "", "de-DE,de;q=0.9,en;q=0.8", "de"},
		{"q values rank the request languages", "", "fr;q=0.9,en;q=0.5,de;q=0.7", "de"},
		{"unsupported request languages are skipped", "", "fr-FR,es;q=0.8,en;q=0.2", "en"},
		{"q=0 means not acceptable", "", "de;q=0,fr", defaultMailLocale},
		{"nothing known falls back to the instance default", "", "", defaultMailLocale},
		{"an unknown stored value counts as none", "xx", "de", "de"},
		{"no request, no stored language", "", "", defaultMailLocale},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := mailLocale(c.stored, c.header); got != c.want {
				t.Fatalf("mailLocale(%q, %q) = %q, want %q", c.stored, c.header, got, c.want)
			}
		})
	}
}

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
			"verify": h.verifyMail("a@example.org", loc, "mia", "tok").Body,
			"reset":  h.resetMail("a@example.org", loc, "mia", "tok").Body,
			"alert":  h.alertMail("a@example.org", loc, "mia").Body,
		} {
			if !strings.HasPrefix(body, tc.marker) {
				t.Errorf("%s mail for stored=%q header=%q starts %q, want %q", name, tc.stored, tc.header, body[:8], tc.marker)
			}
		}
	}
}
