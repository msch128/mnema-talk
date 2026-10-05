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
		{"Herzog", "", true},
		{"Herzog", "long-enough-password", true},
		{"He", "", false},
		{strings.Repeat("a", 33), "", false},
		{"bad name", "", false},
		{"all", "", false},
		{"HERE", "", false},
		{"Herzog", "short", false},
		{"Herzog", strings.Repeat("x", MaxPasswordBytes+1), false},
	}
	for _, c := range cases {
		err := ValidateAdminSeed(c.user, c.pass)
		if (err == nil) != c.ok {
			t.Errorf("ValidateAdminSeed(%q, %d bytes) = %v, want ok=%v", c.user, len(c.pass), err, c.ok)
		}
	}
	if err := ValidateAdminSeed("Herzog", "short"); err == nil || !strings.Contains(err.Error(), "ADMIN_INITIAL_PASSWORD") {
		t.Errorf("error should name the setting: %v", err)
	}
}
