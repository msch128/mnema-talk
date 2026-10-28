//go:build integration

package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRegisterReturnsContextErrorWhenTheClientLeftDuringHashing(t *testing.T) {
	p, _, _ := authFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// The context ends right after the invite pre-check, before the hash.
	fault := authHookPool(t, p, &authQueryHook{match: "SELECT EXISTS (SELECT 1 FROM invites", after: cancel})
	u, err := Register(ctx, fault, "latecomer", "", testPassword, "member-invite-k7x2")
	if u != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("register = %+v, %v; want no user and context.Canceled", u, err)
	}
}

func TestChangePasswordDoesNotOverwriteAConcurrentReset(t *testing.T) {
	p, _, member := authFixture(t)
	ctx := context.Background()
	// An Owner sets a new password after the change verified the old one.
	fault := authHookPool(t, p, &authQueryHook{match: "SELECT 1 FROM users WHERE id = $1 AND password_hash", before: func() {
		if err := SetPassword(ctx, p, member.ID, "owner-chosen-password"); err != nil {
			t.Error(err)
		}
	}})
	if _, err := ChangePassword(ctx, fault, member.ID, testPassword, "stale-tab-password"); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("change after a reset = %v, want ErrWrongPassword", err)
	}
	if _, _, err := Login(ctx, p, member.Username, "owner-chosen-password"); err != nil {
		t.Fatalf("the reset password was overwritten: %v", err)
	}
}

func TestCredentialChangesAreAllOrNothing(t *testing.T) {
	p, _, member := authFixture(t)
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		call func(*testing.T) error
	}{
		{"change password", func(t *testing.T) error {
			_, err := ChangePassword(ctx, authHookPool(t, p, &authQueryHook{match: "UPDATE account_tokens SET used_at", cancel: true}), member.ID, testPassword, "next-password-value")
			return err
		}},
		{"set password", func(t *testing.T) error {
			return SetPassword(ctx, authHookPool(t, p, &authQueryHook{match: "UPDATE account_tokens SET used_at", cancel: true}), member.ID, "next-password-value")
		}},
		{"reset password", func(t *testing.T) error {
			_, err := ResetPassword(ctx, authHookPool(t, p, &authQueryHook{match: "UPDATE account_tokens SET used_at", cancel: true}), member.ID)
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(t); !errors.Is(err, context.Canceled) {
				t.Fatalf("error = %v, want cancellation", err)
			}
			// A change reported as failed took no effect.
			if _, version, err := Login(ctx, p, member.Username, testPassword); err != nil || version != 0 {
				t.Fatalf("failed change left a trace: version=%d err=%v", version, err)
			}
		})
	}
}

func TestResetTwoFactorIsAllOrNothing(t *testing.T) {
	p, _, member := authFixture(t)
	ctx := context.Background()
	if _, err := p.Exec(ctx, `INSERT INTO user_totp (user_id, secret_enc, confirmed_at) VALUES ($1, '\x00', NOW())`, member.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Exec(ctx, `INSERT INTO recovery_codes (user_id, code_hash) VALUES ($1, '\x01')`, member.ID); err != nil {
		t.Fatal(err)
	}
	// The last statement of the removal fails.
	fault := authHookPool(t, p, &authQueryHook{match: "DELETE FROM user_totp", cancel: true})
	if err := ResetTwoFactor(ctx, fault, member.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("reset = %v, want cancellation", err)
	}
	var codes, version int
	if err := p.QueryRow(ctx, `SELECT (SELECT COUNT(*) FROM recovery_codes WHERE user_id = $1), token_version FROM users WHERE id = $1`, member.ID).Scan(&codes, &version); err != nil {
		t.Fatal(err)
	}
	if codes != 1 || version != 0 {
		t.Fatalf("partial reset: recovery codes=%d token_version=%d", codes, version)
	}
	if err := ResetTwoFactor(ctx, p, member.ID); err != nil {
		t.Fatal(err)
	}
}

func TestIdentityChecksThePasswordBeforeTakingTheFactorLock(t *testing.T) {
	p, _, member := authFixture(t)
	var h *Handler
	free := false
	hook := &authQueryHook{match: "SELECT password_hash FROM users WHERE id = $1 AND", before: func() {
		m := &h.factorLocks[int(member.ID[15])%len(h.factorLocks)]
		if free = m.TryLock(); free {
			m.Unlock()
		}
	}}
	h = NewHandler(&Sessions{DB: authHookPool(t, p, hook), Secret: secret, TTL: time.Hour}, nil)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	if ok, err := h.ConfirmIdentity(w, r, member, testPassword, ""); !ok || err != nil {
		t.Fatalf("confirm = %v, %v", ok, err)
	}
	if !free {
		t.Fatal("the password check ran while the shared factor lock was held")
	}
}
