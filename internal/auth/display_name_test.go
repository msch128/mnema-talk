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
