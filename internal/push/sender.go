package push

import (
	"context"
	"errors"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
	"github.com/msch128/mnema-talk/internal/ssrf"
)

// Outcome is how a push service answered one delivery.
type Outcome string

const (
	// Delivered: the push service accepted the message (200, 201 or 202).
	Delivered Outcome = "ok"
	// Gone: the subscription no longer exists (404, 410); remove it.
	Gone Outcome = "gone"
	// Retry: a temporary refusal (429, 5xx) worth one more attempt.
	Retry Outcome = "retry"
	// Rejected: the service refused the request (other 4xx).
	Rejected Outcome = "rejected"
	// Failed: no answer: the address is not allowed, the name does not
	// resolve, the connection or TLS failed, or it timed out.
	Failed Outcome = "error"
)

// Result is one delivery attempt. Reason is a short code, never a raw network
// error, so logs and the test button cannot be used to probe a network.
type Result struct {
	Outcome    Outcome
	Reason     string
	RetryAfter time.Duration
}

// Urgency and lifetime of a push (RFC 8030 section 5).
type Options struct {
	TTL     time.Duration
	Urgent  bool
	Topic   string
	Timeout time.Duration
}

// HTTPClient posts a prepared push request; production passes the SSRF-guarded client.
type HTTPClient = webpush.HTTPClient

// Sender signs and encrypts pushes and posts them to the subscription's push
// service through HTTP, which must be the SSRF-guarded client in production.
type Sender struct {
	Keys    Keys
	Subject string
	HTTP    HTTPClient
}

// maxAnswerBytes bounds how much of a push service's answer is read.
const maxAnswerBytes = 4 << 10

// Send delivers payload to one subscription. The payload is encrypted for the
// browser (aes128gcm) and the request carries the VAPID signature.
func (s *Sender) Send(ctx context.Context, t target, payload []byte, o Options) Result {
	if o.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, o.Timeout)
		defer cancel()
	}
	urgency := webpush.UrgencyNormal
	if o.Urgent {
		urgency = webpush.UrgencyHigh
	}
	// The library pads the payload inside the slice it is given, so every
	// send gets a private copy: one payload goes to several devices at once.
	res, err := webpush.SendNotificationWithContext(ctx, slices.Clone(payload),
		&webpush.Subscription{Endpoint: t.endpoint, Keys: webpush.Keys{P256dh: t.p256dh, Auth: t.auth}},
		&webpush.Options{
			HTTPClient:      s.HTTP,
			Subscriber:      subscriber(s.Subject),
			Topic:           o.Topic,
			TTL:             int(o.TTL.Seconds()),
			Urgency:         urgency,
			VAPIDPublicKey:  s.Keys.Public,
			VAPIDPrivateKey: s.Keys.Private,
		})
	if err != nil {
		return Result{Outcome: Failed, Reason: classifyError(err)}
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, maxAnswerBytes))
	return classifyStatus(res)
}

func classifyStatus(res *http.Response) Result {
	reason := "http_" + strconv.Itoa(res.StatusCode)
	switch {
	case res.StatusCode == http.StatusCreated || res.StatusCode == http.StatusAccepted || res.StatusCode == http.StatusOK:
		return Result{Outcome: Delivered, Reason: reason}
	case res.StatusCode == http.StatusNotFound || res.StatusCode == http.StatusGone:
		return Result{Outcome: Gone, Reason: reason}
	case res.StatusCode == http.StatusTooManyRequests || res.StatusCode >= 500:
		return Result{Outcome: Retry, Reason: reason, RetryAfter: retryAfter(res.Header.Get("Retry-After"), time.Now())}
	}
	return Result{Outcome: Rejected, Reason: reason}
}

// retryAfter reads a Retry-After header (seconds or an HTTP date), capped at
// maxRetryDelay: the one retry never waits longer than that.
func retryAfter(header string, now time.Time) time.Duration {
	header = strings.TrimSpace(header)
	var wait time.Duration
	if seconds, err := strconv.Atoi(header); err == nil {
		wait = time.Duration(seconds) * time.Second
	} else if at, err := http.ParseTime(header); err == nil {
		wait = at.Sub(now)
	}
	return min(max(wait, 0), maxRetryDelay)
}

// classifyError names why no answer came back.
func classifyError(err error) string {
	switch {
	case errors.Is(err, ssrf.ErrBlocked):
		return "blocked_address"
	case errors.Is(err, ssrf.ErrResolve):
		return "dns_error"
	case errors.Is(err, ssrf.ErrConnect):
		return "connect_error"
	case errors.Is(err, ssrf.ErrBadURL):
		return "invalid_url"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, webpush.ErrMaxPadExceeded):
		return "payload_too_large"
	}
	var timeout interface{ Timeout() bool }
	if errors.As(err, &timeout) && timeout.Timeout() {
		return "timeout"
	}
	if msg := err.Error(); strings.Contains(msg, "tls:") || strings.Contains(msg, "x509:") {
		return "tls_error"
	}
	return "request_error"
}
