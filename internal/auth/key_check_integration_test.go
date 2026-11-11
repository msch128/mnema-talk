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

func TestAnAbandonedEnrolmentUnderALostKeyDoesNotStopTheRotation(t *testing.T) {
	p, admin, member := authFixture(t)
	ctx := context.Background()
	enrolTwoFactor(t, &Sessions{DB: p, Secret: sessionSecretA}, p, admin.ID)
	// The member started the setup under a secret that no longer exists and
	// never confirmed it.
	if _, err := (&Sessions{DB: p, Secret: strings.Repeat("c", 40)}).BeginEnrolment(ctx, p, member.ID); err != nil {
		t.Fatal(err)
	}

	rotated := &Sessions{DB: p, Secret: sessionSecretB, PreviousSecret: sessionSecretA}
	res, err := rotated.ResealSecrets(ctx, p)
	if err != nil || res.Moved != 1 || len(res.Unreadable) != 0 || strings.Join(res.Abandoned, ",") != member.Username {
		t.Fatalf("rotate: %+v %v", res, err)
	}
	var left int
	if err := p.QueryRow(ctx, `SELECT COUNT(*) FROM user_totp WHERE user_id = $1`, member.ID).Scan(&left); err != nil || left != 0 {
		t.Fatalf("the worthless row is gone: %d %v", left, err)
	}
	// The member can start again.
	if _, err := rotated.BeginEnrolment(ctx, p, member.ID); err != nil {
		t.Fatal(err)
	}
	if res, err := rotated.ResealSecrets(ctx, p); err != nil || res.Current != 2 || len(res.Abandoned) != 0 {
		t.Fatalf("the next run: %+v %v", res, err)
	}
}

func TestAnUnreadableConfirmedSecretStillStopsTheRotationAndKeepsEveryRow(t *testing.T) {
	p, admin, member := authFixture(t)
	ctx := context.Background()
	lost := &Sessions{DB: p, Secret: strings.Repeat("c", 40)}
	enrolTwoFactor(t, lost, p, admin.ID)
	if _, err := lost.BeginEnrolment(ctx, p, member.ID); err != nil {
		t.Fatal(err)
	}
	res, err := (&Sessions{DB: p, Secret: sessionSecretB, PreviousSecret: sessionSecretA}).ResealSecrets(ctx, p)
	if !errors.Is(err, ErrUnreadableSecrets) || strings.Join(res.Unreadable, ",") != admin.Username || len(res.Abandoned) != 0 {
		t.Fatalf("%+v %v", res, err)
	}
	var rows int
	if err := p.QueryRow(ctx, `SELECT COUNT(*) FROM user_totp`).Scan(&rows); err != nil || rows != 2 {
		t.Fatalf("a refused run deletes nothing: %d %v", rows, err)
	}
}

func TestASignInSucceedsWhenTheSecretCannotBeSealedAgain(t *testing.T) {
	p, admin, _ := authFixture(t)
	ctx := context.Background()
	secret, _ := enrolTwoFactor(t, &Sessions{DB: p, Secret: sessionSecretA}, p, admin.ID)

	// The database refuses to store a new blob; spending a step still works.
	if _, err := p.Exec(ctx, `CREATE FUNCTION refuse_reseal() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'reseal refused'; END $$`); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Exec(ctx, `CREATE TRIGGER refuse_reseal BEFORE UPDATE ON user_totp FOR EACH ROW WHEN (NEW.secret_enc IS DISTINCT FROM OLD.secret_enc) EXECUTE FUNCTION refuse_reseal()`); err != nil {
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
	if _, err := p.Exec(ctx, `UPDATE user_totp SET last_step = 0 WHERE user_id = $1`, admin.ID); err != nil {
		t.Fatal(err)
	}
	code := totpCode(secret, totpStep(time.Now()))
	res, err := withKey.VerifyFactor(ctx, p, admin.ID, code)
	if err != nil || !res.OK {
		t.Fatalf("a correct code while the move fails: %+v %v", res, err)
	}
	if storedKeyID(t, p, admin.ID) != 0 {
		t.Fatal("the refused move left the old blob in place")
	}
	// The step is spent although the move failed: the code works once.
	if res, err := withKey.VerifyFactor(ctx, p, admin.ID, code); err != nil || res.OK {
		t.Fatalf("the same code again: %+v %v", res, err)
	}

	drop()
	if !signInWithAuthenticator(t, withKey, p, admin.ID, secret) || storedKeyID(t, p, admin.ID) != dataKeyID(dataKeyOne) {
		t.Fatal("the next sign-in moves the secret")
	}
}

func TestTheStoredSecretCheckCountsWhatNoConfiguredKeyOpens(t *testing.T) {
	p, admin, member := authFixture(t)
	ctx := context.Background()
	withKey := &Sessions{DB: p, Secret: sessionSecretA, DataKey: dataKeyOne}
	enrolTwoFactor(t, withKey, p, admin.ID)
	enrolTwoFactor(t, &Sessions{DB: p, Secret: sessionSecretA}, p, member.ID)

	check := func(s *Sessions) StoredSecrets {
		t.Helper()
		found, err := s.CheckStoredSecrets(ctx, p)
		if err != nil {
			t.Fatal(err)
		}
		return found
	}
	if found := check(withKey); found != (StoredSecrets{}) {
		t.Fatalf("every key is configured: %+v", found)
	}
	// DATA_KEY is taken out again: the admin's blob names a key nobody has.
	if found := check(&Sessions{DB: p, Secret: sessionSecretA}); found != (StoredSecrets{Unreadable: 1, UnknownDataKey: 1}) {
		t.Fatalf("DATA_KEY removed: %+v", found)
	}
	// The session secret is replaced without naming the old one, too.
	if found := check(&Sessions{DB: p, Secret: sessionSecretB}); found != (StoredSecrets{Unreadable: 2, UnknownDataKey: 1}) {
		t.Fatalf("both keys gone: %+v", found)
	}
	// Naming the data key as the previous one is the way back.
	if found := check(&Sessions{DB: p, Secret: sessionSecretA, DataKeyPrevious: dataKeyOne}); found != (StoredSecrets{}) {
		t.Fatalf("the data key named as previous: %+v", found)
	}

	// A setup that was never confirmed protects nothing and is not counted.
	if err := RemoveTwoFactor(ctx, p, member.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := (&Sessions{DB: p, Secret: sessionSecretB}).BeginEnrolment(ctx, p, member.ID); err != nil {
		t.Fatal(err)
	}
	if found := check(withKey); found != (StoredSecrets{}) {
		t.Fatalf("an unconfirmed setup under another key: %+v", found)
	}
}

func TestRotateKeysMovesSecretsBackFromADataKeyThatIsTakenOut(t *testing.T) {
	p, admin, _ := authFixture(t)
	ctx := context.Background()
	secret, codes := enrolTwoFactor(t, &Sessions{DB: p, Secret: sessionSecretA, DataKey: dataKeyOne}, p, admin.ID)

	back := &Sessions{DB: p, Secret: sessionSecretA, DataKeyPrevious: dataKeyOne}
	if res, err := back.ResealSecrets(ctx, p); err != nil || res.Moved != 1 || storedKeyID(t, p, admin.ID) != 0 {
		t.Fatalf("the way back: %+v %v", res, err)
	}
	if res, err := back.VerifyFactor(ctx, p, admin.ID, codes[0]); err != nil || !res.OK {
		t.Fatalf("a recovery code made under the data key, while it is named: %+v %v", res, err)
	}
	if !signInWithAuthenticator(t, &Sessions{DB: p, Secret: sessionSecretA}, p, admin.ID, secret) {
		t.Fatal("sign-in with the session secret alone, after the move")
	}
}

func remainingCodes(t *testing.T, s *Sessions, p *db.Pool, id uuid.UUID) int {
	t.Helper()
	st, err := s.TwoFactorState(context.Background(), p, id)
	if err != nil {
		t.Fatal(err)
	}
	return st.RecoveryCodesRemaining
}

func TestRecoveryCodesUnderARetiredKeyAreNotCountedAsRemaining(t *testing.T) {
	p, admin, member := authFixture(t)
	ctx := context.Background()
	installed := &Sessions{DB: p, Secret: sessionSecretA}
	enrolTwoFactor(t, installed, p, admin.ID)
	enrolTwoFactor(t, installed, p, member.ID)
	// The member's codes are from a release that did not record their key.
	if _, err := p.Exec(ctx, `UPDATE recovery_codes SET key_fingerprint = NULL WHERE user_id = $1`, member.ID); err != nil {
		t.Fatal(err)
	}

	rotating := &Sessions{DB: p, Secret: sessionSecretB, PreviousSecret: sessionSecretA}
	if n := remainingCodes(t, rotating, p, admin.ID); n != 10 {
		t.Fatalf("while the previous secret is configured the codes verify and count: %d", n)
	}
	retired := &Sessions{DB: p, Secret: sessionSecretB}
	if _, err := rotating.ResealSecrets(ctx, p); err != nil {
		t.Fatal(err)
	}
	if n := remainingCodes(t, retired, p, admin.ID); n != 0 {
		t.Fatalf("codes under the retired secret cannot verify and must not be shown as remaining: %d", n)
	}
	if n := remainingCodes(t, retired, p, member.ID); n != 10 {
		t.Fatalf("codes whose key is not recorded stay counted: %d", n)
	}
	// New codes replace the dead ones and count again.
	err := pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		_, err := retired.ReplaceRecoveryCodes(ctx, tx, admin.ID)
		return err
	})
	if err != nil || remainingCodes(t, retired, p, admin.ID) != 10 {
		t.Fatalf("new codes: %v", err)
	}
}
