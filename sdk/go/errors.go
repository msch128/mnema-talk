package mnema

import (
	"errors"
	"fmt"
	"time"
)

// Discord error codes the bot API answers (doc/bots.md, REST API).
const (
	CodeUnknownChannel     = 10003
	CodeUnknownMessage     = 10008
	CodeUnknownUser        = 10013
	CodeUnknownInteraction = 10062
	CodeMissingPermissions = 50013
	CodeEmptyMessage       = 50006
	CodeInvalidFormBody    = 50035
	CodeTooLarge           = 40005
)

// RESTError is a non-2xx answer of the REST API, in Discord's error shape. A
// 429 that persisted beyond the retries also carries RetryAfter and Global.
type RESTError struct {
	// Status is the HTTP status.
	Status int
	// Code is Discord's JSON error code (0 for plain HTTP errors).
	Code int
	// Message is the server's text.
	Message string
	// RetryAfter is how long the server asked to wait (429 only).
	RetryAfter time.Duration
	// Global reports that the bot's overall limit was hit (429 only).
	Global bool
}

func (e *RESTError) Error() string {
	return fmt.Sprintf("mnema: HTTP %d (code %d): %s", e.Status, e.Code, e.Message)
}

// Close codes of the bot gateway (doc/bots.md, Close codes).
const (
	CloseSessionTakenOver   = 4000
	CloseUnknownOpcode      = 4001
	CloseDecodeError        = 4002
	CloseNotAuthenticated   = 4003
	CloseAuthFailed         = 4004
	CloseAlreadyIdentified  = 4005
	CloseRateLimited        = 4008
	CloseSessionTimedOut    = 4009
	CloseInvalidIntents     = 4013
	CloseDisallowedIntents  = 4014
	closeHeartbeatNotAcked  = 4900
	closeCodeFrameTooLarge  = 1009
	closeCodeReadsTooSlowly = 1013
)

// CloseError is returned by Run when the gateway closed with a fatal code
// (4004, 4013, 4014): the token or the intents must change before the bot can
// connect again.
type CloseError struct {
	Code   int
	Reason string
}

func (e *CloseError) Error() string {
	return fmt.Sprintf("mnema: gateway closed with %d: %s", e.Code, closeMeaning(e.Code, e.Reason))
}

func closeMeaning(code int, reason string) string {
	if reason != "" {
		return reason
	}
	switch code {
	case CloseAuthFailed:
		return "authentication failed: bad, revoked or expired token, or the bot is disabled, deleted or banned"
	case CloseInvalidIntents:
		return "invalid intents"
	case CloseDisallowedIntents:
		return "intents the admin has not allowed for this bot"
	case CloseRateLimited:
		return "rate limited"
	case closeHeartbeatNotAcked:
		return "no heartbeat ACK from the server"
	default:
		return "connection closed"
	}
}

// IsFatalClose reports whether a close code stops Run.
func IsFatalClose(code int) bool {
	return code == CloseAuthFailed || code == CloseInvalidIntents || code == CloseDisallowedIntents
}

// ErrAlreadyRunning is returned by Run while another Run of the same client
// is active.
var ErrAlreadyRunning = errors.New("mnema: the client is already running")

// ErrFrameTooLarge is returned for an outgoing gateway frame above 4096 bytes.
var ErrFrameTooLarge = errors.New("mnema: gateway frame larger than 4096 bytes")
