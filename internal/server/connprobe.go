package server

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/sfu"
)

// ConnectionProbeRequest is the body of POST /api/webrtc/probe.
type ConnectionProbeRequest struct {
	// SDP is the browser's offer: one data channel and no audio or video,
	// with all of its candidates gathered.
	SDP string `json:"sdp" maxLength:"16384"`
}

// ConnectionProbeAnswer is the response of POST /api/webrtc/probe.
type ConnectionProbeAnswer struct {
	// SDP is the server's answer with all of its candidates.
	SDP string `json:"sdp"`
}

// connectionProbe handles POST /api/webrtc/probe.
//
// @Summary Answer a connection test
// @Description Backs "Test my connection": answers a browser's data-channel-only offer with a throwaway peer connection that uses the same media sockets and announced addresses as a Talk, so the browser can tell whether its media reaches the server directly or only through the relay. No audio, no media permission, no room and no presence; the connection closes itself after a few seconds. Signed-in members only; at most twelve per ten minutes and eight at a time. Candidates of the offer are vetted like those of a Talk member.
// @ID probeConnection
// @Tags Realtime
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param request body ConnectionProbeRequest true "Request body."
// @Success 200 {object} ConnectionProbeAnswer "The server's answer."
// @Header 200 {string} Cache-Control "no-store"
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): not a data-channel-only offer."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests, or too many tests running."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Failure 503 {object} httpx.ErrorResponse "UNAVAILABLE: the voice engine is not running."
// @Router /api/webrtc/probe [post]
func (h *systemHandler) connectionProbe(w http.ResponseWriter, r *http.Request) error {
	if h.sfu == nil {
		return httpx.ErrUnavailable("voice is not available")
	}
	member := auth.UserFrom(r.Context())
	if ok, retry := h.probeLimit.Allow(member.ID.String()); !ok {
		httpx.WriteRateLimited(w, retry)
		return nil
	}
	var req ConnectionProbeRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	answer, err := h.sfu.Probe(r.Context(), member.ID.String(), req.SDP)
	switch {
	case errors.Is(err, sfu.ErrProbeInvalid):
		return httpx.ErrInvalidInput("the offer must contain one data channel and no audio or video")
	case errors.Is(err, sfu.ErrProbeBusy):
		httpx.WriteRateLimited(w, 5*time.Second)
		return nil
	case sfu.IsClosed(err):
		return httpx.ErrUnavailable("voice is not available")
	case err != nil:
		slog.Warn("connection test failed", "err", err)
		return httpx.ErrInternal("the connection test could not be set up")
	}
	w.Header().Set("Cache-Control", "no-store")
	httpx.WriteJSON(w, http.StatusOK, ConnectionProbeAnswer{SDP: answer})
	return nil
}

// WebSocketReport is the body of POST /api/admin/status/websocket.
type WebSocketReport struct {
	// Connected is whether the admin's own /api/ws connection is open and
	// upgraded.
	Connected bool `json:"connected"`
}

// reportWebSocket handles POST /api/admin/status/websocket.
//
// @Summary Report the admin's WebSocket
// @Description The admin page tells the server whether its own /api/ws connection came through the reverse proxy as an upgraded WebSocket. The Status tab shows the outcome as the WebSocket item. Owner or Administrator only (403 otherwise).
// @ID reportAdminWebSocket
// @Tags Admin
// @Accept json
// @Security cookieAuth
// @Param request body WebSocketReport true "Request body."
// @Success 204 "Recorded."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed body."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: only the Owner and members with the Administrator permission may do this."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/status/websocket [post]
func (h *systemHandler) reportWebSocket(w http.ResponseWriter, r *http.Request) error {
	admin := auth.UserFrom(r.Context())
	if ok, retry := h.wsReportLimit.Allow(admin.ID.String()); !ok {
		httpx.WriteRateLimited(w, retry)
		return nil
	}
	var req WebSocketReport
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	// Only a success is taken from the browser. A socket that is merely not
	// open yet says nothing about the proxy; real misses are counted where
	// they arrive, on /api/ws.
	if req.Connected {
		h.setup.RecordWebSocket(true, time.Now())
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
