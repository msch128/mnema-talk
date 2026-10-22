package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

const (
	recoveryCodeCount = 10
	// recoveryCodeLen characters from a 31 letter alphabet are about 59 bits.
	recoveryCodeLen = 12
	// No i, l, o, 0, 1: codes get read from paper.
	recoveryAlphabet = "abcdefghjkmnpqrstuvwxyz23456789"
)

var (
	errTwoFactorOn  = httpx.ErrConflict("two-factor sign-in is already on")
	errTwoFactorOff = httpx.ErrConflict("two-factor sign-in is not on")
	// ErrWrongCode answers every failed second step alike: wrong, replayed,
	// used, expired and unknown challenges look the same.
	ErrWrongCode = httpx.NewAPIError(401, httpx.CodeInvalidCredentials, "invalid or expired code")
)

// TwoFactorState is what an account has set up.
type TwoFactorState struct {
	Enabled                bool
	RecoveryCodesRemaining int
}

func (s *Sessions) twoFactorState(ctx context.Context, q db.Querier, id uuid.UUID) (TwoFactorState, error) {
	var st TwoFactorState
	err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM user_totp WHERE user_id = $1 AND confirmed_at IS NOT NULL),
		(SELECT COUNT(*) FROM recovery_codes WHERE user_id = $1 AND used_at IS NULL)`, id).
		Scan(&st.Enabled, &st.RecoveryCodesRemaining)
	if !st.Enabled {
		st.RecoveryCodesRemaining = 0
	}
	return st, err
}

// beginEnrolment stores a fresh pending secret (replacing an earlier pending
// one) and returns it. It refuses while two-factor sign-in is on.
func (s *Sessions) beginEnrolment(ctx context.Context, q db.Querier, id uuid.UUID) ([]byte, error) {
	secret, err := newTOTPSecret()
	if err != nil {
		return nil, err
	}
	sealed, err := s.sealSecret(id, secret)
	if err != nil {
		return nil, err
	}
	tag, err := q.Exec(ctx, `INSERT INTO user_totp (user_id, secret_enc) VALUES ($1, $2)
		ON CONFLICT (user_id) DO UPDATE SET secret_enc = $2, last_step = 0, created_at = NOW()
		WHERE user_totp.confirmed_at IS NULL`, id, sealed)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, errTwoFactorOn
	}
	return secret, nil
}

// confirmEnrolment checks the first code against the pending secret, turns
// two-factor sign-in on and returns the one-time recovery codes.
func (s *Sessions) confirmEnrolment(ctx context.Context, tx pgx.Tx, id uuid.UUID, code string) ([]string, error) {
	var sealed []byte
	var confirmed bool
	err := tx.QueryRow(ctx, `SELECT secret_enc, confirmed_at IS NOT NULL FROM user_totp WHERE user_id = $1 FOR UPDATE`, id).Scan(&sealed, &confirmed)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrWrongCode
	}
	if err != nil {
		return nil, err
	}
	if confirmed {
		return nil, errTwoFactorOn
	}
	secret, err := s.openSecret(id, sealed)
	if err != nil {
		return nil, err
	}
	step, ok := matchTOTP(secret, cleanCode(code), time.Now())
	if !ok {
		return nil, ErrWrongCode
	}
	if _, err := tx.Exec(ctx, `UPDATE user_totp SET confirmed_at = NOW(), last_step = $2 WHERE user_id = $1`, id, step); err != nil {
		return nil, err
	}
	return s.replaceRecoveryCodes(ctx, tx, id)
}

// replaceRecoveryCodes deletes every recovery code of the account and makes
// ten new ones; the plain codes are returned once and never stored.
func (s *Sessions) replaceRecoveryCodes(ctx context.Context, q db.Querier, id uuid.UUID) ([]string, error) {
	if _, err := q.Exec(ctx, `DELETE FROM recovery_codes WHERE user_id = $1`, id); err != nil {
		return nil, err
	}
	codes := make([]string, 0, recoveryCodeCount)
	for len(codes) < recoveryCodeCount {
		code, err := newRecoveryCode()
		if err != nil {
			return nil, err
		}
		tag, err := q.Exec(ctx, `INSERT INTO recovery_codes (user_id, code_hash) VALUES ($1, $2) ON CONFLICT DO NOTHING`, id, s.recoveryHash(id, code))
		if err != nil {
			return nil, err
		}
		if tag.RowsAffected() == 1 {
			codes = append(codes, formatRecoveryCode(code))
		}
	}
	return codes, nil
}

func newRecoveryCode() (string, error) {
	out := make([]byte, 0, recoveryCodeLen)
	buf := make([]byte, 2*recoveryCodeLen)
	limit := byte(256 - 256%len(recoveryAlphabet))
	for len(out) < recoveryCodeLen {
		if _, err := rand.Read(buf); err != nil {
			return "", fmt.Errorf("random code: %w", err)
		}
		for _, b := range buf {
			if b < limit && len(out) < recoveryCodeLen {
				out = append(out, recoveryAlphabet[int(b)%len(recoveryAlphabet)])
			}
		}
	}
	return string(out), nil
}

// formatRecoveryCode groups a code for reading: abcd-efgh-jkmn.
func formatRecoveryCode(code string) string {
	return code[0:4] + "-" + code[4:8] + "-" + code[8:12]
}

// verifyFactor checks a code typed at sign-in or before a sensitive change:
// six digits are an authenticator code (each step is accepted once), anything
// else a recovery code (each is accepted once). usedRecovery tells which.
//
// Wrong codes are counted in the database (see two_factor_failures.go): when a
// kind's daily budget is spent it is refused with a 429 without looking at the
// code. Unreported is how many failures of the account the member has not been
// told about yet, after this call.
type factorResult struct {
	OK         bool
	Recovery   bool
	Unreported int
}

func (s *Sessions) verifyFactor(ctx context.Context, q db.Querier, id uuid.UUID, input string) (factorResult, error) {
	code := cleanCode(input)
	kind, check := kindRecovery, s.useRecoveryCode
	if isTOTPShape(code) {
		kind, check = kindTOTP, s.verifyTOTP
	}
	if err := s.failureBudget(ctx, q, id, kind); err != nil {
		return factorResult{}, err
	}
	ok, err := check(ctx, q, id, code)
	if err != nil || ok {
		return factorResult{OK: ok, Recovery: ok && kind == kindRecovery}, err
	}
	n, err := s.noteFailure(ctx, q, id, kind)
	return factorResult{Unreported: n}, err
}

func (s *Sessions) verifyTOTP(ctx context.Context, q db.Querier, id uuid.UUID, code string) (bool, error) {
	var sealed []byte
	err := q.QueryRow(ctx, `SELECT secret_enc FROM user_totp WHERE user_id = $1 AND confirmed_at IS NOT NULL`, id).Scan(&sealed)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	secret, err := s.openSecret(id, sealed)
	if err != nil {
		return false, err
	}
	step, ok := matchTOTP(secret, code, time.Now())
	if !ok {
		return false, nil
	}
	// One atomic update spends the step: a second use of the same code, or any
	// older one, finds last_step already at or above it.
	tag, err := q.Exec(ctx, `UPDATE user_totp SET last_step = $2 WHERE user_id = $1 AND last_step < $2`, id, step)
	return err == nil && tag.RowsAffected() == 1, err
}

func (s *Sessions) useRecoveryCode(ctx context.Context, q db.Querier, id uuid.UUID, code string) (bool, error) {
	if len(code) != recoveryCodeLen || strings.Trim(code, recoveryAlphabet) != "" {
		return false, nil
	}
	tag, err := q.Exec(ctx, `UPDATE recovery_codes SET used_at = NOW()
		WHERE user_id = $1 AND code_hash = $2 AND used_at IS NULL`, id, s.recoveryHash(id, code))
	return err == nil && tag.RowsAffected() == 1, err
}

// removeTwoFactor turns the second factor off and forgets everything about it.
func removeTwoFactor(ctx context.Context, q db.Querier, id uuid.UUID) error {
	for _, stmt := range []string{
		`DELETE FROM login_challenges WHERE user_id = $1`,
		`DELETE FROM second_factor_failures WHERE user_id = $1`,
		`DELETE FROM recovery_codes WHERE user_id = $1`,
		`DELETE FROM user_totp WHERE user_id = $1`,
	} {
		if _, err := q.Exec(ctx, stmt, id); err != nil {
			return err
		}
	}
	return nil
}
