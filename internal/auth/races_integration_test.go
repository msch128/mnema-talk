//go:build integration

package auth

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/testutil/racetest"
)

// Lane 7 of the defect hunt: limits and "only one" under callers released
// together, with an invariant query after every round.

const raceCallers = 8

func liveTokens(t *testing.T, p *db.Pool, user uuid.UUID, purpose string) int {
	t.Helper()
	var n int
	if err := p.QueryRow(context.Background(), `SELECT COUNT(*) FROM account_tokens
		WHERE user_id = $1 AND purpose = $2 AND used_at IS NULL`, user, purpose).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// The hourly budget in the database: a caller that finds it unspent issues a
// link, as both mail paths do (IssuedTooOften, then IssueAccountToken).
func issueWithinBudget(ctx context.Context, p *db.Pool, user uuid.UUID, purpose string) error {
	tooOften, err := IssuedTooOften(ctx, p, user, purpose)
	if err != nil {
		return err
	}
	if tooOften {
		return errBudgetSpent
	}
	_, err = IssueAccountToken(ctx, p, user, purpose, "", time.Hour)
	return err
}

var errBudgetSpent = fmt.Errorf("budget spent")

// mailLinkCallers is four times TokensPerHour, so most callers queue behind
// the lock and find the budget spent there.
const mailLinkCallers = 20

// spentOnly reports whether every error of the round is a refusal of the
// hourly budget (a timeout or a pool error would be something else).
func spentOnly(r racetest.Round) bool {
	for _, err := range r.Errs {
		if err != nil && !errors.Is(err, errBudgetSpent) && !errors.Is(err, ErrTooManyLinks) {
			return false
		}
	}
	return true
}

// sweepMailLinks races mailLinkCallers issues for one account and purpose and
// returns the rounds that left more than one live link and those that issued
// more or fewer than TokensPerHour links in the hour.
func sweepMailLinks(t *testing.T, purpose string) (racetest.Result, racetest.Result) {
	p, _, member := authFixture(t)
	ctx := context.Background()
	reset := func(int) {
		if _, err := p.Exec(ctx, `DELETE FROM account_tokens`); err != nil {
			t.Fatal(err)
		}
	}
	rounds := racetest.Rounds(20, 500)
	live := racetest.Sweep(t, rounds, mailLinkCallers, reset,
		func(_, _ int) error { return issueWithinBudget(ctx, p, member.ID, purpose) },
		func(round int, r racetest.Round) string {
			if n := liveTokens(t, p, member.ID, purpose); n != 1 || !spentOnly(r) {
				return fmt.Sprintf("round %d: %d live links, errors %v", round, n, r.Errs)
			}
			return ""
		})
	budget := racetest.Sweep(t, rounds, mailLinkCallers, reset,
		func(_, _ int) error { return issueWithinBudget(ctx, p, member.ID, purpose) },
		func(round int, r racetest.Round) string {
			n, err := recentTokens(ctx, p, member.ID, purpose)
			if err != nil || n != TokensPerHour || r.Succeeded() != TokensPerHour || !spentOnly(r) {
				return fmt.Sprintf("round %d: %d links in the hour, %d issues succeeded (%v, %v)", round, n, r.Succeeded(), err, r.Errs)
			}
			return ""
		})
	return live, budget
}

// H-L7-01: the count, the retire and the insert of IssueAccountToken run
// under one lock per account and purpose.
func TestParallelMailLinksLeaveOneLiveLinkWithinTheHourlyBudget(t *testing.T) {
	for _, purpose := range []string{PurposePasswordReset, PurposeEmailVerify} {
		t.Run(purpose, func(t *testing.T) {
			live, budget := sweepMailLinks(t, purpose)
			live.FailOnViolation(t)
			budget.FailOnViolation(t)
		})
	}
}

func TestParallelUsesOfOneMailLinkLetExactlyOneWin(t *testing.T) {
	p, _, member := authFixture(t)
	ctx := context.Background()
	var plain string
	racetest.Sweep(t, racetest.Rounds(20, 500), raceCallers,
		func(int) {
			// A fresh hourly budget per round: IssueAccountToken refuses the
			// sixth link of the hour.
			if _, err := p.Exec(ctx, `DELETE FROM account_tokens`); err != nil {
				t.Fatal(err)
			}
			var err error
			if plain, err = IssueAccountToken(ctx, p, member.ID, PurposePasswordReset, "", time.Hour); err != nil {
				t.Fatal(err)
			}
		},
		func(_, _ int) error {
			_, err := ConsumeToken(ctx, p, plain, PurposePasswordReset)
			return err
		},
		func(round int, r racetest.Round) string {
			if r.Succeeded() != 1 {
				return fmt.Sprintf("round %d: %d uses of one link succeeded", round, r.Succeeded())
			}
			return ""
		}).FailOnViolation(t)
}

func TestParallelSignupsNeverPassTheInviteLimit(t *testing.T) {
	p, admin, _ := authFixture(t)
	ctx := context.Background()
	const limit = 3
	var code string
	racetest.Sweep(t, racetest.Rounds(5, 60), raceCallers,
		func(round int) {
			code = fmt.Sprintf("race-invite-%d-k7x2q", round)
			max := limit
			if _, err := CreateInvite(ctx, p, admin.ID, code, &max, nil); err != nil {
				t.Fatal(err)
			}
		},
		func(round, i int) error {
			_, err := Register(ctx, p, fmt.Sprintf("racer_%d_%d", round, i), "", testPassword, code)
			return err
		},
		func(round int, r racetest.Round) string {
			var uses, users int
			if err := p.QueryRow(ctx, `SELECT i.uses_count, (SELECT COUNT(*) FROM users u WHERE u.invite_id = i.id)
				FROM invites i WHERE code = $1`, code).Scan(&uses, &users); err != nil {
				t.Fatal(err)
			}
			if uses > limit || users > limit || uses != users || r.Succeeded() != users {
				return fmt.Sprintf("round %d: limit %d, uses_count %d, accounts %d, successes %d", round, limit, uses, users, r.Succeeded())
			}
			return ""
		}).FailOnViolation(t)
}

func TestParallelSignInsWithOneSecondFactorCodeLetExactlyOneWin(t *testing.T) {
	p, admin, _ := authFixture(t)
	ctx := context.Background()
	s := &Sessions{DB: p, Secret: sessionSecretA}
	secret, codes := enrolTwoFactor(t, s, p, admin.ID)
	var code string
	racetest.Sweep(t, racetest.Rounds(len(codes)*2, len(codes)*2), raceCallers,
		func(round int) {
			if _, err := p.Exec(ctx, `UPDATE user_totp SET last_step = 0 WHERE user_id = $1`, admin.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := p.Exec(ctx, `DELETE FROM second_factor_failures`); err != nil {
				t.Fatal(err)
			}
			code = totpCode(secret, totpStep(time.Now()))
			if round%2 == 1 {
				code = codes[round/2]
			}
		},
		func(_, _ int) error {
			res, err := s.VerifyFactor(ctx, p, admin.ID, code)
			if err == nil && !res.OK {
				return ErrWrongCode
			}
			return err
		},
		func(round int, r racetest.Round) string {
			if r.Succeeded() != 1 {
				return fmt.Sprintf("round %d (recovery %v): %d sign-ins with one code succeeded", round, round%2 == 1, r.Succeeded())
			}
			return ""
		}).FailOnViolation(t)
}

// Two processes starting on an empty database (for example an updater that
// starts the new container before the old one has stopped) each seed the
// Owner. Exactly one Owner must result, and a start that lost the race to an
// Owner of the same name must not fail.
func TestParallelOwnerSeedsLeaveExactlyOneOwner(t *testing.T) {
	ctx := context.Background()
	for _, sameName := range []bool{true, false} {
		t.Run(fmt.Sprintf("same name %v", sameName), func(t *testing.T) {
			var p *db.Pool
			res := racetest.Sweep(t, racetest.Rounds(10, 200), 2,
				func(int) {
					p, _, _ = authFixture(t)
					if _, err := p.Exec(ctx, `DELETE FROM users WHERE role = 'admin'`); err != nil {
						t.Fatal(err)
					}
				},
				func(_, i int) error {
					name := "owner"
					if !sameName {
						name = fmt.Sprintf("owner_%d", i)
					}
					return EnsureAdminUser(ctx, p, name, testPassword, true)
				},
				func(round int, r racetest.Round) string {
					var owners int
					if err := p.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE role = 'admin'`).Scan(&owners); err != nil {
						t.Fatal(err)
					}
					if owners != 1 || r.Succeeded() != 2 {
						return fmt.Sprintf("round %d: %d owners, start errors %v", round, owners, r.Errs)
					}
					return ""
				})
			// Expected failure until H-L7-02 is fixed: hasAdminUser and the
			// insert are two statements and nothing in the schema allows
			// only one Owner.
			res.KnownFailure(t, "H-L7-02")
		})
	}
}
