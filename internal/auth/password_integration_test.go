//go:build integration

package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"strings"
	"testing"

	"github.com/msch128/mnema-talk/internal/httpx"
	"golang.org/x/crypto/bcrypt"
)

func TestResetPasswordUserRemovedBeforeWrite(t *testing.T) {
	p, _, member := authFixture(t)
	fault := authHookPool(t, p, &authQueryHook{match: "UPDATE users SET password_hash", before: func() {
		if _, e := p.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, member.ID); e != nil {
			t.Fatal(e)
		}
	}})
	password, err := ResetPassword(context.Background(), fault, member.ID)
	api, ok := httpx.AsAPIError(err)
	if password != "" || !ok || api.Status != 404 {
		t.Fatalf("reset after deletion = %q, %v", password, err)
	}
}

func TestPasswordCostHookRejectsOutOfRangeValues(t *testing.T) {
	original := PasswordCost()
	for _, cost := range []int{bcrypt.MinCost - 1, bcrypt.MaxCost + 1} {
		t.Run("invalid cost", func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("invalid cost did not panic")
				}
				if PasswordCost() != original {
					t.Error("invalid cost changed active cost")
				}
			}()
			SetPasswordCostForTests(cost)
		})
	}
}

func TestAuthHashEntropyFailuresPreserveAccounts(t *testing.T) {
	p, admin, member := authFixture(t)
	ctx := context.Background()
	for _, tc := range []struct {
		name      string
		available int
		call      func() error
	}{
		{"register", 0, func() error {
			_, err := Register(ctx, p, "entropy_member", "", testPassword, "member-invite-k7x2")
			return err
		}},
		{"change password", 0, func() error {
			_, err := ChangePassword(ctx, p, member.ID, testPassword, "next-password-value")
			return err
		}},
		{"set password", 0, func() error { return SetPassword(ctx, p, member.ID, "next-password-value") }},
		// rand.Read gets exactly enough entropy for a 15-byte temporary
		// password; bcrypt's subsequent 16-byte salt read encounters EOF.
		{"reset password", 15, func() error { _, err := ResetPassword(ctx, p, member.ID); return err }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			func() {
				original := rand.Reader
				defer func() { rand.Reader = original }()
				rand.Reader = bytes.NewReader(make([]byte, tc.available))
				if err := tc.call(); err == nil || !strings.Contains(err.Error(), "hash password") {
					t.Fatalf("entropy failure = %v", err)
				}
			}()
			if _, tv, err := Login(ctx, p, member.Username, testPassword); err != nil || tv != 0 {
				t.Fatalf("failed hash changed account: tv=%d err=%v", tv, err)
			}
		})
	}
	if _, err := p.Exec(ctx, `DELETE FROM users WHERE id=$1`, admin.ID); err != nil {
		t.Fatal(err)
	}
	func() {
		original := rand.Reader
		defer func() { rand.Reader = original }()
		rand.Reader = bytes.NewReader(nil)
		if err := EnsureAdminUser(ctx, p, "entropy-admin", testPassword, true); err == nil || !strings.Contains(err.Error(), "hash admin password") {
			t.Fatalf("seed entropy failure = %v", err)
		}
	}()
	var exists bool
	if err := p.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE username IN ('entropy_member','entropy-admin'))`).Scan(&exists); err != nil || exists {
		t.Fatalf("failed hash created account: exists=%v err=%v", exists, err)
	}
}
