//go:build integration

package auth

import (
	"context"
	"errors"
	"testing"
)

// A legacy custom code below the floor no longer registers anyone; a
// generated code does.
func TestRegisterRefusesWeakLegacyInvites(t *testing.T) {
	p, admin, _ := authFixture(t)
	ctx := context.Background()
	if _, err := p.Exec(ctx, `INSERT INTO invites (code, created_by) VALUES ('welcome', $1)`, admin.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := Register(ctx, p, "latecomer", "", testPassword, "welcome"); !errors.Is(err, ErrInvalidInvite) {
		t.Fatalf("weak legacy invite: %v, want ErrInvalidInvite", err)
	}
	inv, err := CreateInvite(ctx, p, admin.ID, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Register(ctx, p, "latecomer", "", testPassword, inv.Code); err != nil {
		t.Fatalf("generated invite: %v", err)
	}
}
