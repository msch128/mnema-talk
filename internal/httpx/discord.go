package httpx

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// DiscordError is the error body of the bot API, in Discord's shape.
type DiscordError struct {
	Message string `json:"message"`
	// Code is Discord's JSON error code: 0 for plain HTTP errors, 10003
	// unknown channel, 10008 unknown message, 50001 missing access, 50006
	// empty message, 50013 missing permissions, 50035 invalid form body,
	// 40005 request entity too large.
	Code int `json:"code"`
}

// DiscordRateLimit is the 429 body of the bot API, in Discord's shape.
type DiscordRateLimit struct {
	Message string `json:"message" enums:"You are being rate limited."`
	// RetryAfter is the number of seconds to wait.
	RetryAfter float64 `json:"retry_after"`
	// Global is true when the bot's overall limit was hit, false for the
	// limit of one route.
	Global bool `json:"global"`
}

// WriteDiscordRateLimit answers 429 in Discord's shape with Retry-After.
func WriteDiscordRateLimit(w http.ResponseWriter, retry time.Duration, global bool) {
	w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())+1))
	if global {
		w.Header().Set("X-RateLimit-Global", "true")
	}
	w.Header().Set("X-RateLimit-Scope", "user")
	WriteJSON(w, http.StatusTooManyRequests, DiscordRateLimit{Message: "You are being rate limited.", RetryAfter: retry.Seconds(), Global: global})
}

// WriteDiscordError renders err in Discord's error shape. Anything that is
// not an *APIError collapses to a logged, sanitized 500.
func WriteDiscordError(w http.ResponseWriter, err error) {
	ae, ok := AsAPIError(err)
	if !ok {
		ae = ErrServer(err)
	}
	WriteJSON(w, ae.Status, DiscordError{Message: ae.Message, Code: discordCode(ae)})
}

// discordCode maps an error to the nearest Discord JSON error code.
func discordCode(e *APIError) int {
	switch e.Status {
	case http.StatusBadRequest:
		if strings.Contains(e.Message, "empty") {
			return 50006
		}
		return 50035
	case http.StatusForbidden:
		return 50013
	case http.StatusNotFound:
		switch {
		case strings.Contains(e.Message, "channel"):
			return 10003
		case strings.Contains(e.Message, "message"):
			return 10008
		case strings.Contains(e.Message, "member"), strings.Contains(e.Message, "user"):
			return 10013
		}
	case http.StatusRequestEntityTooLarge:
		return 40005
	}
	return 0
}

// HandleDiscord is Handle with Discord-shaped errors, for the bot API.
func HandleDiscord(h HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := h(w, r); err != nil {
			if _, ok := AsAPIError(err); !ok {
				slog.ErrorContext(r.Context(), "request failed",
					"request_id", RequestIDFromContext(r.Context()), "method", r.Method, "path", LogPath(r.URL.Path), "err", err)
				err = ErrInternal("internal server error")
			}
			WriteDiscordError(w, err)
		}
	}
}
