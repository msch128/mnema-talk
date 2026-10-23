package auth

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/mailer"
)

// Wrong second-factor codes are kept in the database, so a restart does not
// reset the count and an attacker who knows the password cannot keep guessing
// six-digit codes at the pace of the in-memory lockout (about 500 a day).
//
//   - totpFailuresPerDay: failed authenticator codes per rolling 24 hours, then
//     authenticator codes are refused for the account until the oldest failure
//     ages out. Recovery codes still work.
//   - recoveryFailuresPerDay: failed recovery codes, a separate and much higher
//     budget: a recovery code has about 59 bits, so guessing is infeasible, and
//     the limit exists to bound the work, not to lock the owner out.
//   - noticeAfter: this many failures not yet reported make the next
//     successful sign-in show a security notice, and send one e-mail.
const (
	totpFailuresPerDay     = 20
	recoveryFailuresPerDay = 100
	noticeAfter            = 5
	failureWindow          = 24 * time.Hour
	failureKeep            = 30 * 24 * time.Hour
)

const (
	kindTOTP     = "totp"
	kindRecovery = "recovery"
)

// SecurityNotice tells the member that wrong second-factor codes were entered
// for the account since the last time they signed in.
type SecurityNotice struct {
	FailedAttempts int `json:"failed_attempts"`
}

// TwoFactorLoginResult is the answer to a finished second step.
type TwoFactorLoginResult struct {
	User User `json:"user"`
	// SecurityNotice is set once when at least five wrong codes were entered
	// since the last sign-in.
	SecurityNotice *SecurityNotice `json:"security_notice,omitempty" binding:"optional"`
}

func capError(retry time.Duration) error {
	e := httpx.NewAPIError(429, httpx.CodeRateLimited, "too many wrong codes today: use a recovery code")
	e.RetryAfter = retry
	return e
}

// failureBudget refuses a kind of code whose daily budget is spent.
func (s *Sessions) failureBudget(ctx context.Context, q db.Querier, id uuid.UUID, kind string) error {
	limit := totpFailuresPerDay
	if kind == kindRecovery {
		limit = recoveryFailuresPerDay
	}
	var n int
	var oldest *time.Time
	err := q.QueryRow(ctx, `SELECT COUNT(*), MIN(at) FROM (
		SELECT at FROM second_factor_failures WHERE user_id = $1 AND kind = $2 AND at > NOW() - $3 * INTERVAL '1 second'
		ORDER BY at DESC LIMIT $4) recent`, id, kind, int64(failureWindow.Seconds()), limit).Scan(&n, &oldest)
	if err != nil || n < limit || oldest == nil {
		return err
	}
	return capError(time.Until(oldest.Add(failureWindow)))
}

// noteFailure stores one wrong code and returns how many failures of the
// account have not been reported to the member yet.
func (s *Sessions) noteFailure(ctx context.Context, q db.Querier, id uuid.UUID, kind string) (int, error) {
	if _, err := q.Exec(ctx, `DELETE FROM second_factor_failures WHERE at < NOW() - $1 * INTERVAL '1 second'`, int64(failureKeep.Seconds())); err != nil {
		return 0, err
	}
	if _, err := q.Exec(ctx, `INSERT INTO second_factor_failures (user_id, kind) VALUES ($1, $2)`, id, kind); err != nil {
		return 0, err
	}
	var n int
	err := q.QueryRow(ctx, `SELECT COUNT(*) FROM second_factor_failures WHERE user_id = $1 AND NOT noticed`, id).Scan(&n)
	return n, err
}

// takeNotice marks every unreported failure as reported and returns the notice
// to show, or nil when fewer than noticeAfter had piled up.
func (s *Sessions) takeNotice(ctx context.Context, q db.Querier, id uuid.UUID) (*SecurityNotice, error) {
	var n int
	err := q.QueryRow(ctx, `WITH marked AS (UPDATE second_factor_failures SET noticed = TRUE
		WHERE user_id = $1 AND NOT noticed RETURNING 1) SELECT COUNT(*) FROM marked`, id).Scan(&n)
	if err != nil || n < noticeAfter {
		return nil, err
	}
	return &SecurityNotice{FailedAttempts: n}, nil
}

// raiseAlert audits the failure streak and, when the account has a confirmed
// address and the server can send mail, tells its owner. It runs once per
// streak, when the count reaches noticeAfter. Neither the log nor the mail
// holds a code.
func (h *Handler) raiseAlert(ctx context.Context, u *User) {
	h.auditUser(ctx, u, "auth.two_factor_alert")
	if !h.mailEnabled() {
		return
	}
	h.background(func(bg context.Context) {
		st, err := loadEmailState(bg, h.Sessions.DB, u.ID)
		if err != nil || st.Address == "" || !st.Verified {
			return
		}
		_ = h.sendMail(bg, h.alertMail(st.Address, mailLocale(st.Locale, ""), st.Username))
	})
}

func (h *Handler) alertMail(to, locale, username string) mailer.Message {
	if locale == "de" {
		return mailer.Message{To: to, Subject: serviceName + ": falsche Anmeldecodes für dein Konto", Body: "Hallo " + username + ",\n\n" +
			"für dein Konto bei " + serviceName + " wurden mehrfach falsche Codes für die Zwei-Faktor-Anmeldung eingegeben. " +
			"Jemand kennt möglicherweise dein Passwort. Ändere es und prüfe deine Recovery-Codes unter Konto & Sicherheit.\n"}
	}
	return mailer.Message{To: to, Subject: serviceName + ": wrong sign-in codes for your account", Body: "Hello " + username + ",\n\n" +
		"several wrong two-factor codes were entered for your account at " + serviceName + ". " +
		"Somebody may know your password. Change it and review your recovery codes under Account & security.\n"}
}

func logFailureError(err error) {
	if err != nil {
		slog.Warn("second-factor failure bookkeeping failed", "err", err)
	}
}
