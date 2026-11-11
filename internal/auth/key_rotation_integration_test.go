//go:build integration

package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
)

// enrolTwoFactor turns two-factor sign-in on for id and returns the
// authenticator secret and the recovery codes.
func enrolTwoFactor(t *testing.T, s *Sessions, p *db.Pool, id uuid.UUID) ([]byte, []string) {
	t.Helper()
	ctx := context.Background()
	secret, err := s.BeginEnrolment(ctx, p, id)
	if err != nil {
		t.Fatal(err)
	}
	var codes []string
	err = pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		codes, err = s.ConfirmEnrolment(ctx, tx, id, totpCode(secret, totpStep(time.Now())))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return secret, codes
}

// signInWithAuthenticator is the second step of a sign-in with the current code.
func signInWithAuthenticator(t *testing.T, s *Sessions, p *db.Pool, id uuid.UUID, secret []byte) bool {
	t.Helper()
	ctx := context.Background()
	// Forget the step the last sign-in spent, so the same 30 seconds work again.
	if _, err := p.Exec(ctx, `UPDATE user_totp SET last_step = 0 WHERE user_id = $1`, id); err != nil {
		t.Fatal(err)
	}
	res, err := s.VerifyFactor(ctx, p, id, totpCode(secret, totpStep(time.Now())))
	if err != nil && err != errUnreadableSecret {
		t.Fatal(err)
	}
	return res.OK
}

func storedKeyID(t *testing.T, p *db.Pool, id uuid.UUID) uint32 {
	t.Helper()
	var sealed []byte
	if err := p.QueryRow(context.Background(), `SELECT secret_enc FROM user_totp WHERE user_id = $1`, id).Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	keyID, _, _ := parseSealed(sealed)
	return keyID
}

func TestRotatingTheDataKeyKeepsTwoFactorSignInWorking(t *testing.T) {
	p, admin, member := authFixture(t)
	ctx := context.Background()

	// The instance as it was installed: no DATA_KEY.
	installed := &Sessions{DB: p, Secret: sessionSecretA}
	adminSecret, adminCodes := enrolTwoFactor(t, installed, p, admin.ID)
	memberSecret, memberCodes := enrolTwoFactor(t, installed, p, member.ID)
	if storedKeyID(t, p, admin.ID) != 0 {
		t.Fatal("without DATA_KEY the stored blob carries no key id")
	}

	// DATA_KEY is set. The next sign-in moves that account's secret.
	withKey := &Sessions{DB: p, Secret: sessionSecretA, DataKey: dataKeyOne}
	if !signInWithAuthenticator(t, withKey, p, admin.ID, adminSecret) {
		t.Fatal("sign-in after DATA_KEY was set")
	}
	if storedKeyID(t, p, admin.ID) != dataKeyID(dataKeyOne) || storedKeyID(t, p, member.ID) != 0 {
		t.Fatal("the secret that was used is sealed again under DATA_KEY, the other waits")
	}

	// DATA_KEY is rotated, and rotate-keys moves everything that is left.
	rotated := &Sessions{DB: p, Secret: sessionSecretA, DataKey: dataKeyTwo, DataKeyPrevious: dataKeyOne}
	res, err := rotated.ResealSecrets(ctx, p)
	if err != nil || res.Moved != 2 || res.Current != 0 || len(res.Unreadable) != 0 || res.RecoveryAccounts != 2 {
		t.Fatalf("rotate: %+v %v", res, err)
	}
	for _, id := range []uuid.UUID{admin.ID, member.ID} {
		if storedKeyID(t, p, id) != dataKeyID(dataKeyTwo) {
			t.Fatal("a blob is still under an old key")
		}
	}
	if again, err := rotated.ResealSecrets(ctx, p); err != nil || again.Moved != 0 || again.Current != 2 {
		t.Fatalf("a second run has nothing to do: %+v %v", again, err)
	}
	// Recovery codes from before both changes are checked with the previous keys.
	if res, err := rotated.VerifyFactor(ctx, p, admin.ID, adminCodes[0]); err != nil || !res.OK || !res.Recovery {
		t.Fatalf("a recovery code from before the rotation: %+v %v", res, err)
	}
	if res, err := rotated.VerifyFactor(ctx, p, admin.ID, adminCodes[0]); err != nil || res.OK {
		t.Fatalf("a recovery code works once: %+v %v", res, err)
	}

	// The previous keys are removed, and the session secret is replaced too.
	final := &Sessions{DB: p, Secret: sessionSecretB, DataKey: dataKeyTwo}
	if !signInWithAuthenticator(t, final, p, admin.ID, adminSecret) || !signInWithAuthenticator(t, final, p, member.ID, memberSecret) {
		t.Fatal("sign-in after the previous keys were removed")
	}
	if res, err := final.ResealSecrets(ctx, p); err != nil || res.Current != 2 || len(res.Unreadable) != 0 {
		t.Fatalf("nothing depends on a removed key: %+v %v", res, err)
	}
	// Decision D6: old recovery codes end with the key they were made under; new ones work.
	if res, err := final.VerifyFactor(ctx, p, member.ID, memberCodes[0]); err != nil || res.OK {
		t.Fatalf("a recovery code under a retired key: %+v %v", res, err)
	}
	fresh, err := final.ReplaceRecoveryCodes(ctx, p, member.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res, err := final.VerifyFactor(ctx, p, member.ID, fresh[0]); err != nil || !res.OK {
		t.Fatalf("a new recovery code: %+v %v", res, err)
	}
}

func TestRotatingTheSessionSecretAloneKeepsTwoFactorSignInWorking(t *testing.T) {
	p, admin, _ := authFixture(t)
	secret, codes := enrolTwoFactor(t, &Sessions{DB: p, Secret: sessionSecretA}, p, admin.ID)

	if signInWithAuthenticator(t, &Sessions{DB: p, Secret: sessionSecretB}, p, admin.ID, secret) {
		t.Fatal("without the previous secret the stored secret must not open")
	}
	rotated := &Sessions{DB: p, Secret: sessionSecretB, PreviousSecret: sessionSecretA}
	if !signInWithAuthenticator(t, rotated, p, admin.ID, secret) {
		t.Fatal("sign-in after the session secret was rotated")
	}
	if res, err := rotated.VerifyFactor(context.Background(), p, admin.ID, codes[0]); err != nil || !res.OK {
		t.Fatalf("recovery code: %+v %v", res, err)
	}
	// That sign-in sealed the secret again: the previous secret can go.
	if !signInWithAuthenticator(t, &Sessions{DB: p, Secret: sessionSecretB}, p, admin.ID, secret) {
		t.Fatal("sign-in after the previous secret was removed")
	}
}

func TestRotateKeysNamesTheAccountsItCannotRead(t *testing.T) {
	p, admin, member := authFixture(t)
	ctx := context.Background()
	enrolTwoFactor(t, &Sessions{DB: p, Secret: sessionSecretA, DataKey: dataKeyOne}, p, admin.ID)
	enrolTwoFactor(t, &Sessions{DB: p, Secret: sessionSecretA}, p, member.ID)

	// The key the admin's secret is under is not configured any more.
	res, err := (&Sessions{DB: p, Secret: sessionSecretA, DataKey: dataKeyTwo}).ResealSecrets(ctx, p)
	if !errors.Is(err, ErrUnreadableSecrets) || res.Moved != 0 || strings.Join(res.Unreadable, ",") != admin.Username {
		t.Fatalf("%+v %v", res, err)
	}
	if storedKeyID(t, p, admin.ID) != dataKeyID(dataKeyOne) || storedKeyID(t, p, member.ID) != 0 {
		t.Fatal("with an unreadable secret nothing may move, the readable ones included")
	}
}

func TestRotateKeysMovesAllSecretsOrNone(t *testing.T) {
	p, admin, member := authFixture(t)
	ctx := context.Background()
	installed := &Sessions{DB: p, Secret: sessionSecretA}
	enrolTwoFactor(t, installed, p, admin.ID)
	enrolTwoFactor(t, installed, p, member.ID)

	// The database refuses the second of the two updates (the rows are moved
	// in the order of the account names: cool_guy, then member).
	if _, err := p.Exec(ctx, `CREATE FUNCTION refuse_reseal() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'reseal refused'; END $$`); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Exec(ctx, `CREATE TRIGGER refuse_reseal BEFORE UPDATE ON user_totp FOR EACH ROW WHEN (NEW.user_id = '`+member.ID.String()+`') EXECUTE FUNCTION refuse_reseal()`); err != nil {
		t.Fatal(err)
	}
	dropped := false
	drop := func() {
		if !dropped {
			_, _ = p.Exec(ctx, `DROP TRIGGER refuse_reseal ON user_totp; DROP FUNCTION refuse_reseal()`)
			dropped = true
		}
	}
	t.Cleanup(drop)

	withKey := &Sessions{DB: p, Secret: sessionSecretA, DataKey: dataKeyOne}
	res, err := withKey.ResealSecrets(ctx, p)
	if err == nil || !strings.Contains(err.Error(), "reseal refused") || res.Moved != 0 {
		t.Fatalf("a failure half way: %+v %v", res, err)
	}
	if storedKeyID(t, p, admin.ID) != 0 || storedKeyID(t, p, member.ID) != 0 {
		t.Fatal("a failed run left a secret moved")
	}

	drop()
	if res, err := withKey.ResealSecrets(ctx, p); err != nil || res.Moved != 2 {
		t.Fatalf("the repeated run: %+v %v", res, err)
	}
	if res, err := withKey.ResealSecrets(ctx, p); err != nil || res.Moved != 0 || res.Current != 2 {
		t.Fatalf("a run with nothing left to do: %+v %v", res, err)
	}
}

func TestRotatingTheSessionSecretKeepsSessionsUntilTheyExpire(t *testing.T) {
	p, admin, _ := authFixture(t)
	before := &Sessions{DB: p, Secret: sessionSecretA, TTL: time.Hour}
	old, _ := IssueToken(admin.ID, 0, before.Secret, before.TTL)
	expired, _ := IssueToken(admin.ID, 0, before.Secret, -time.Minute)
	foreign, _ := IssueToken(admin.ID, 0, strings.Repeat("c", 40), time.Hour)

	rotated := &Sessions{DB: p, Secret: sessionSecretB, PreviousSecret: sessionSecretA, TTL: time.Hour}
	if u, err := rotated.Authenticate(requestWithToken(rotated, old)); err != nil || u.ID != admin.ID {
		t.Fatalf("a session from before the rotation: %v", err)
	}
	if id, exp := rotated.SessionClaims(requestWithToken(rotated, old)); id == "" || exp.IsZero() {
		t.Fatal("a long-lived connection of that session needs its id and expiry")
	}
	if _, err := rotated.Authenticate(requestWithToken(rotated, expired)); err == nil {
		t.Fatal("the rotation must not extend an expired session")
	}
	if _, err := rotated.Authenticate(requestWithToken(rotated, foreign)); err == nil {
		t.Fatal("a session signed with a key that is neither current nor previous")
	}

	// The previous secret is removed: sessions signed since stay, older ones end.
	fresh, _ := IssueToken(admin.ID, 0, rotated.Secret, rotated.TTL)
	after := &Sessions{DB: p, Secret: sessionSecretB, TTL: time.Hour}
	if _, err := after.Authenticate(requestWithToken(after, fresh)); err != nil {
		t.Fatalf("a session signed after the rotation: %v", err)
	}
	if _, err := after.Authenticate(requestWithToken(after, old)); err == nil {
		t.Fatal("a session under the removed secret")
	}

	// Signing out of a session from before the rotation still ends it.
	if err := rotated.Revoke(requestWithToken(rotated, old)); err != nil {
		t.Fatal(err)
	}
	if _, err := rotated.Authenticate(requestWithToken(rotated, old)); err == nil {
		t.Fatal("a signed-out session from before the rotation still works")
	}
}
