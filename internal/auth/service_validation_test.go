package auth

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestServiceValidationRejectsBeforeDatabaseAccess(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct{ username, displayName, password, invite string }{
		{"ab", "", testPassword, "code123"},
		{"validuser", "", "short", "code123"},
		{"validuser", strings.Repeat("x", MaxDisplayNameLen+1), testPassword, "code123"},
		{"validuser", "", testPassword, ""},
		{"validuser", "", testPassword, strings.Repeat("x", maxInviteCodeChars+1)},
	} {
		if u, err := Register(ctx, nil, tc.username, tc.displayName, tc.password, tc.invite); u != nil || err == nil {
			t.Fatalf("invalid registration = %+v, %v", u, err)
		}
	}
	if _, err := ChangePassword(ctx, nil, uuid.Nil, testPassword, "short"); err == nil {
		t.Fatal("short new password accepted")
	}
	if _, err := UpdateProfile(ctx, nil, uuid.Nil, strings.Repeat("x", MaxDisplayNameLen+1), ""); err == nil {
		t.Fatal("long display name accepted")
	}
	if _, err := UpdateProfile(ctx, nil, uuid.Nil, "valid", strings.Repeat("x", MaxBioLen+1)); err == nil {
		t.Fatal("long bio accepted")
	}
	if _, err := SetPresence(ctx, nil, uuid.Nil, "offline"); err == nil {
		t.Fatal("offline presence accepted")
	}
	if _, err := SetStatusText(ctx, nil, uuid.Nil, strings.Repeat("x", MaxStatusTextLen+1), nil); err == nil {
		t.Fatal("long status accepted")
	}
}
