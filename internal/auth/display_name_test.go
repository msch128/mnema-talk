package auth

import (
	"strings"
	"testing"
)

func TestDefaultDisplayNameTruncatesLongUsername(t *testing.T) {
	username := strings.Repeat("a", 32)
	if got := defaultDisplayName(username); got != username[:MaxDisplayNameLen] {
		t.Fatalf("display name = %q", got)
	}
}

// A display name is how others read a member, also inside their messages
// (mention pills): the characters that render differently from what they
// spell go, as for bot and webhook names.
func TestDisplayNamesDropInvisibleAndDirectionCharacters(t *testing.T) {
	for _, in := range []string{"\u202eevil", "ev\u200bil", "\u2066evil\u2069", "evil\u061c"} {
		got, err := cleanDisplayName(in)
		if err != nil || got != "evil" {
			t.Errorf("cleanDisplayName(%q) = %q, %v; want \"evil\"", in, got, err)
		}
		_, name, _, err := cleanRegistration("evilname", in, "a long enough passphrase 1", "code")
		if err != nil || name != "evil" {
			t.Errorf("registration with %q: %q, %v", in, name, err)
		}
	}
	if _, err := cleanDisplayName("two\nlines"); err == nil {
		t.Error("a line break in a display name was accepted")
	}
	if got, err := cleanDisplayName("Zoë 👩\u200d💻"); err != nil || got != "Zoë 👩\u200d💻" {
		t.Errorf("a plain name with an emoji sequence changed: %q, %v", got, err)
	}
}
