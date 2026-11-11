//go:build integration

package auth

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/httpx"
)

func TestAuthNotFoundAndConflicts(t *testing.T) {
	p, admin, member := authFixture(t)
	ctx := context.Background()
	for name, call := range map[string]func() error{
		"locale":              func() error { _, e := SetLocale(ctx, p, uuid.New(), "en"); return e },
		"presence":            func() error { _, e := SetPresence(ctx, p, uuid.New(), PresenceAway); return e },
		"status":              func() error { _, e := SetStatusText(ctx, p, uuid.New(), "hello", nil); return e },
		"reset":               func() error { _, e := ResetPassword(ctx, p, uuid.New()); return e },
		"set password target": func() error { return SetPassword(ctx, p, uuid.New(), testPassword) },
	} {
		t.Run(name, func(t *testing.T) {
			e, ok := httpx.AsAPIError(call())
			if !ok || e.Status != 404 {
				t.Fatalf("error=%+v want404", e)
			}
		})
	}
	if err := SetPassword(ctx, p, member.ID, "short"); err == nil {
		t.Fatal("short password accepted")
	}
	if _, err := Register(ctx, p, "newmember", "", testPassword, "unknown-code"); err != ErrInvalidInvite {
		t.Fatalf("unknown invite = %v", err)
	}
	if _, err := Register(ctx, p, "MEMBER", "", testPassword, "member-invite-k7x2"); err != ErrUsernameTaken {
		t.Fatalf("case-insensitive conflict = %v", err)
	}
	if err := SetPassword(ctx, p, admin.ID, testPassword); err == nil {
		t.Fatal("admin password target accepted")
	}
}
