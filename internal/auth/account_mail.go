package auth

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/mailer"
)

// MailSender delivers account e-mails. The server leaves Handler.Mail nil
// when SMTP is not configured; every flow that needs mail is then off.
type MailSender interface {
	Send(ctx context.Context, msg mailer.Message) error
}

// mailJobs bounds the mail work done in the background, and mailJobTimeout
// one such job.
const (
	mailJobs       = 8
	mailJobTimeout = 30 * time.Second
)

// recovery holds the state of the e-mail based account flows: their rate
// limits and the bounded background mail queue.
type recovery struct {
	requestPerIP   *httpx.RateLimiter
	requestPerSite *httpx.RateLimiter
	confirmPerIP   *httpx.RateLimiter
	verifyPerIP    *httpx.RateLimiter
	optionsPerIP   *httpx.RateLimiter
	// perAddress caps the confirmation mails one address gets an hour, however
	// many accounts ask. It is keyed by a hash of the address, so it tells
	// nothing about accounts.
	perAddress *httpx.RateLimiter
	// resetFlood is a coarse flood guard on the anonymous reset form, keyed
	// the same way whether or not an account has the address.
	resetFlood *httpx.RateLimiter
	// resetPerAccount is the exact, race-free cap on reset mails per account:
	// the database count alone lets concurrent requests slip past together. It
	// is a budget of its own, so reset requests of strangers never spend the
	// confirmation mails' perAddress budget.
	resetPerAccount *httpx.RateLimiter
	resendPerUser   *httpx.RateLimiter
	// downloadPerUser bounds export downloads.
	downloadPerUser *httpx.RateLimiter
	slots           chan struct{}
	wg              sync.WaitGroup
}

func newRecovery() *recovery {
	return &recovery{
		requestPerIP:    httpx.NewRateLimiter(10, time.Hour).Named("reset_request"),
		requestPerSite:  httpx.NewRateLimiter(40, time.Hour).Named("reset_request_site"),
		confirmPerIP:    httpx.NewRateLimiter(20, time.Hour).Named("reset_confirm"),
		verifyPerIP:     httpx.NewRateLimiter(30, time.Hour).Named("email_verify"),
		perAddress:      httpx.NewRateLimiter(5, time.Hour),
		resetFlood:      httpx.NewRateLimiter(30, time.Hour),
		resetPerAccount: httpx.NewRateLimiter(tokensPerHour, time.Hour),
		optionsPerIP:    httpx.NewRateLimiter(120, 15*time.Minute).Named("auth_options"),
		resendPerUser:   httpx.NewRateLimiter(1, time.Minute),
		downloadPerUser: httpx.NewRateLimiter(10, time.Hour).Named("export_download"),
		slots:           make(chan struct{}, mailJobs),
	}
}

// mountRecovery registers the unauthenticated e-mail based routes.
func (h *Handler) mountRecovery(r chi.Router) {
	r.With(h.rec.optionsPerIP.PerIP).Get("/auth/options", httpx.Handle(h.authOptions))
	r.With(h.rec.requestPerSite.By(httpx.ClientAggregateKey), h.rec.requestPerIP.PerIP).
		Post("/auth/password-reset/request", httpx.Handle(h.requestPasswordReset))
	r.With(h.rec.confirmPerIP.PerIP).Post("/auth/password-reset/confirm", httpx.Handle(h.confirmPasswordReset))
	r.With(h.rec.verifyPerIP.PerIP).Post("/auth/email/verify", httpx.Handle(h.verifyEmail))
}

// mountAccount registers the signed-in routes for the account's e-mail.
func (h *Handler) mountAccount(r chi.Router) {
	r.Get("/users/me/account", httpx.Handle(h.getAccount))
	r.Put("/users/me/email", httpx.Handle(h.setEmail))
	r.Post("/users/me/email/remove", httpx.Handle(h.removeEmailHandler))
	r.Post("/users/me/email/resend", httpx.Handle(h.resendVerification))
	h.mountTwoFactor(r)
	h.mountExport(r)
	r.Post("/users/me/delete", httpx.Handle(h.deleteAccount))
}

// mailEnabled reports whether account e-mails can be sent.
func (h *Handler) mailEnabled() bool { return h.Mail != nil }

// background runs fn off the request, so the time a request takes does not
// depend on whether mail is sent. When the queue is full the job is dropped;
// that happens for any request alike and says nothing about accounts.
func (h *Handler) background(fn func(ctx context.Context)) {
	select {
	case h.rec.slots <- struct{}{}:
	default:
		slog.Warn("mail queue full, dropping account mail")
		return
	}
	h.rec.wg.Add(1)
	go func() {
		defer func() {
			<-h.rec.slots
			h.rec.wg.Done()
		}()
		ctx, cancel := context.WithTimeout(context.Background(), mailJobTimeout)
		defer cancel()
		fn(ctx)
	}()
}

// DrainMail waits for the mail jobs in flight (shutdown and tests).
func (h *Handler) DrainMail() { h.rec.wg.Wait() }

// sendMail delivers msg and logs a failure without the address.
func (h *Handler) sendMail(ctx context.Context, msg mailer.Message) error {
	err := h.Mail.Send(ctx, msg)
	if err != nil {
		slog.Warn("account mail not sent", "err", err)
	}
	return err
}
