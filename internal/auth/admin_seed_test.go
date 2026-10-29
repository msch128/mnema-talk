package auth

import (
	"strings"
	"testing"
)

func TestValidateAdminSeed(t *testing.T) {
	cases := []struct {
		user, pass string
		ok         bool
	}{
		{"Cool_Guy", "", true},
		{"Cool_Guy", "long-enough-password", true},
		{"He", "", false},
		{strings.Repeat("a", 33), "", false},
		{"bad name", "", false},
		{"all", "", false},
		{"HERE", "", false},
		{"Cool_Guy", "short", false},
		{"Cool_Guy", strings.Repeat("x", MaxPasswordBytes+1), false},
	}
	for _, c := range cases {
		err := ValidateAdminSeed(c.user, c.pass)
		if (err == nil) != c.ok {
			t.Errorf("ValidateAdminSeed(%q, %d bytes) = %v, want ok=%v", c.user, len(c.pass), err, c.ok)
		}
	}
	if err := ValidateAdminSeed("Cool_Guy", "short"); err == nil || !strings.Contains(err.Error(), "ADMIN_INITIAL_PASSWORD") {
		t.Errorf("error should name the setting: %v", err)
	}
}
