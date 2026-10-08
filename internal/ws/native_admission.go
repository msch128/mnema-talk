package ws

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// NativeAuthenticator is implemented by the isolated native auth handler.
// Browser authentication and origin policy remain distinct on the same Hub.
type NativeAuthenticator interface {
	AuthenticateNativeRequest(*http.Request) (auth.NativeLease, error)
	RevalidateNativeLease(context.Context, auth.NativePrincipal) (auth.NativeLease, error)
	AuthenticateAccessLease(context.Context, string) (auth.NativeLease, error)
}

func nativeHeaderPresent(r *http.Request, name string) bool {
	for key := range r.Header {
		if strings.EqualFold(key, name) {
			return true
		}
	}
	return false
}

func nativeHandshakeAllowed(r *http.Request) bool {
	return !nativeHeaderPresent(r, "Origin") && !nativeHeaderPresent(r, "Cookie") &&
		!nativeHeaderPresent(r, "Sec-WebSocket-Protocol") && r.URL.RawQuery == "" && r.URL.User == nil && r.URL.Fragment == ""
}

// NativeWebSocketHandler is explicitly unmounted in the production server.
// Its constructor shares Hub membership, while using native-only authorization.
func (h *Hub) NativeWebSocketHandler(a NativeAuthenticator) http.Handler {
	connectionsPerIP := httpx.NewRateLimiter(30, time.Minute)
	upgrader := websocket.Upgrader{ReadBufferSize: 4096, WriteBufferSize: 4096, CheckOrigin: nativeHandshakeAllowed}
	core := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if a == nil || h.isClosed() {
			httpx.WriteError(w, httpx.ErrUnavailable("native socket unavailable"))
			return
		}
		if !nativeHandshakeAllowed(r) {
			httpx.WriteError(w, httpx.ErrForbidden("native transport request rejected"))
			return
		}
		if r.Body != nil {
			var one [1]byte
			if n, err := r.Body.Read(one[:]); n != 0 || err != io.EOF {
				httpx.WriteError(w, httpx.ErrInvalidInput("native socket body must be empty"))
				return
			}
		}
		original, err := a.AuthenticateNativeRequest(r)
		if err != nil {
			writeNativeAuthError(w, err)
			return
		}
		if h.isClosed() {
			httpx.WriteError(w, httpx.ErrUnavailable("server shutting down"))
			return
		}
		conn, err := upgrader.Upgrade(w, r, http.Header{"Cache-Control": []string{"no-store"}})
		if err != nil {
			return
		} // Gorilla already answered; never log request/header data.
		principal := original.Principal()
		c := newClient(h, conn, principal.User(), principal.TokenVersion())
		c.native = newNativeSocketState(a, original)
		if !h.registerPending(c) {
			c.shutdown()
			c.close()
			return
		}
		fresh, err := a.RevalidateNativeLease(r.Context(), principal)
		if err != nil || !principal.SameNativeAccess(fresh.Principal()) || !c.native.install(1, fresh, false) || !h.registerVerified(c, ptrNativeUser(fresh.Principal())) {
			h.cancelPending(c)
			return
		}
		go c.writePump()
		go c.readPump()
		go c.nativeWatchdog()
	})
	limited := connectionsPerIP.PerIP(core)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		limited.ServeHTTP(w, r)
	})
}

func ptrNativeUser(p auth.NativePrincipal) *auth.User { u := p.User(); return &u }

func writeNativeAuthError(w http.ResponseWriter, err error) {
	if api, ok := httpx.AsAPIError(err); ok {
		httpx.WriteError(w, api)
		return
	}
	// The service exposes redacted errors, and this boundary never logs raw
	// downstream errors even if a future implementation violates that contract.
	httpx.WriteError(w, httpx.ErrUnauthorized("native socket authorization failed"))
}
