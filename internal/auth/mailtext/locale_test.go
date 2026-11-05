package mailtext

import (
	"strings"
	"testing"
)

var supported = map[string]bool{"de": true, "en": true}

func TestMailLocaleRule(t *testing.T) {
	cases := []struct {
		name, stored, header, want string
	}{
		{"stored language wins over the request", "de", "en-US,en;q=0.9", "de"},
		{"stored english wins over a german request", "en", "de-DE,de;q=0.9", "en"},
		{"no stored language uses the request", "", "de-DE,de;q=0.9,en;q=0.8", "de"},
		{"q values rank the request languages", "", "fr;q=0.9,en;q=0.5,de;q=0.7", "de"},
		{"unsupported request languages are skipped", "", "fr-FR,es;q=0.8,en;q=0.2", "en"},
		{"q=0 means not acceptable", "", "de;q=0,fr", DefaultLocale},
		{"nothing known falls back to the instance default", "", "", DefaultLocale},
		{"an unknown stored value counts as none", "xx", "de", "de"},
		{"no request, no stored language", "", "", DefaultLocale},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Locale(c.stored, c.header, supported); got != c.want {
				t.Fatalf("Locale(%q, %q) = %q, want %q", c.stored, c.header, got, c.want)
			}
		})
	}
}

// The mails call the service by the community's name, not by the product's.
func TestMailsUseTheCommunityName(t *testing.T) {
	m := Test("a@example.org", "en", "Lantern Fox")
	if m.FromName != "Lantern Fox" || !strings.Contains(m.Subject, "Lantern Fox") {
		t.Fatalf("test mail = %+v", m)
	}
}
