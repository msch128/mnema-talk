// Package httpx holds the shared HTTP plumbing every handler package builds on,
// so request/response conventions stay identical across the API surface. The
// shapes follow mnema.xyz's internal/http package:
//
//   - Error model: *APIError, the stable CodeXxx machine codes, the ErrXxx
//     constructors and ErrServer (logs the raw error, returns a sanitized 500
//     so database/storage detail never leaks to clients).
//   - Responses: WriteJSON / WriteError and the strict DecodeJSON.
//   - Middleware: request id, Recover, Logger, MaxBody, SecurityHeaders, CORS,
//     same-origin CSRF gate, rate limiting and the escalating login lockout.
package httpx

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
)

// Stable machine-readable error codes. Clients branch on these strings, so
// existing values must not change.
const (
	CodeInvalidInput         = "INVALID_INPUT"
	CodeUnauthorized         = "UNAUTHORIZED"
	CodeInvalidCredentials   = "INVALID_CREDENTIALS"
	CodeForbidden            = "FORBIDDEN"
	CodeAccountDisabled      = "ACCOUNT_DISABLED"
	CodeNotFound             = "NOT_FOUND"
	CodeConflict             = "CONFLICT"
	CodePayloadTooLarge      = "PAYLOAD_TOO_LARGE"
	CodeUnsupportedMediaType = "UNSUPPORTED_MEDIA_TYPE"
	CodeRateLimited          = "RATE_LIMITED"
	CodeUnavailable          = "UNAVAILABLE"
	CodeInternalError        = "INTERNAL_ERROR"
)

// APIError is the canonical handler error. Status drives the response code and
// is never serialized; Code and Message form the client-facing body:
//
//	{"error": {"code": "FORBIDDEN", "message": "..."}}
type APIError struct {
	Status  int    `json:"-"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *APIError) Error() string { return e.Message }

func NewAPIError(status int, code, msg string) *APIError {
	return &APIError{Status: status, Code: code, Message: msg}
}

func ErrInvalidInput(msg string) *APIError {
	return NewAPIError(http.StatusBadRequest, CodeInvalidInput, msg)
}

func ErrUnauthorized(msg string) *APIError {
	return NewAPIError(http.StatusUnauthorized, CodeUnauthorized, msg)
}

func ErrForbidden(msg string) *APIError {
	return NewAPIError(http.StatusForbidden, CodeForbidden, msg)
}

func ErrNotFound(msg string) *APIError {
	return NewAPIError(http.StatusNotFound, CodeNotFound, msg)
}

func ErrConflict(msg string) *APIError {
	return NewAPIError(http.StatusConflict, CodeConflict, msg)
}

func ErrPayloadTooLarge(msg string) *APIError {
	return NewAPIError(http.StatusRequestEntityTooLarge, CodePayloadTooLarge, msg)
}

func ErrUnsupportedMediaType(msg string) *APIError {
	return NewAPIError(http.StatusUnsupportedMediaType, CodeUnsupportedMediaType, msg)
}

func ErrRateLimited() *APIError {
	return NewAPIError(http.StatusTooManyRequests, CodeRateLimited, "too many requests")
}

func ErrUnavailable(msg string) *APIError {
	return NewAPIError(http.StatusServiceUnavailable, CodeUnavailable, msg)
}

func ErrInternal(msg string) *APIError {
	return NewAPIError(http.StatusInternalServerError, CodeInternalError, msg)
}

// ErrServer logs err and returns a sanitized 500. Use it whenever a failure
// originates from a downstream system (Postgres, S3) whose message must not
// reach the client.
func ErrServer(err error) *APIError {
	slog.Error("internal error", "err", err)
	return ErrInternal("internal server error")
}

// AsAPIError unwraps err to an *APIError.
func AsAPIError(err error) (*APIError, bool) {
	var ae *APIError
	if errors.As(err, &ae) {
		return ae, true
	}
	return nil, false
}

// WriteJSON writes v as JSON with the given status.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// WriteError renders err in the error envelope. Anything that is not an
// *APIError collapses to a logged, sanitized 500.
func WriteError(w http.ResponseWriter, err error) {
	ae, ok := AsAPIError(err)
	if !ok {
		ae = ErrServer(err)
	}
	WriteJSON(w, ae.Status, map[string]*APIError{"error": ae})
}

// HandlerFunc is an http.HandlerFunc that returns an error; Handle adapts it so
// handlers can `return err` instead of repeating the error tail.
type HandlerFunc func(w http.ResponseWriter, r *http.Request) error

func Handle(h HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := h(w, r); err != nil {
			if _, ok := AsAPIError(err); !ok {
				slog.ErrorContext(r.Context(), "request failed",
					"request_id", RequestIDFromContext(r.Context()), "method", r.Method, "path", r.URL.Path, "err", err)
				err = ErrInternal("internal server error")
			}
			WriteError(w, err)
		}
	}
}
