package ws

import (
	"context"
	"io"
	"log/slog"
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

func hasNativeHandshakeHeaders(r *http.Request) bool {
	if nativeHeaderPresent(r, "Origin") || nativeHeaderPresent(r, "Cookie") {
		return false
	}
	return !nativeHeaderPresent(r, "Sec-WebSocket-Protocol")
}

func hasCleanURL(r *http.Request) bool {
	if r.URL.RawQuery != "" || r.URL.Fragment != "" {
		return false
	}
	return r.URL.User == nil
}

func nativeHandshakeAllowed(r *http.Request) bool {
	return hasNativeHandshakeHeaders(r) && hasCleanURL(r)
}

// NativeWebSocketHandler is explicitly unmounted in the production server.
// Its constructor shares Hub membership, while using native-only authorization.
func (h *Hub) NativeWebSocketHandler(a NativeAuthenticator) http.Handler {
	return h.nativePumps(a, (*Client).writePump, (*Client).readPump)
}

func (h *Hub) nativePumps(a NativeAuthenticator, write, read func(*Client)) http.Handler {
	return h.nativeSocketHandler(a, func(c *Client) {
		go write(c)
		go read(c)
		go c.nativeWatchdog()
	})
}

func (h *Hub) verifyAndRegisterNative(ctx context.Context, a NativeAuthenticator, c *Client, original auth.NativeLease) bool {
	fresh, err := a.RevalidateNativeLease(ctx, original.Principal())
	if err != nil {
		return false
	}
	if !original.Principal().SameNativeAccess(fresh.Principal()) {
		return false
	}
	if !c.native.install(1, fresh, false) {
		return false
	}
	return h.registerVerified(c, ptrNativeUser(fresh.Principal()))
}

func (h *Hub) admitNativeConn(ctx context.Context, a NativeAuthenticator, conn *websocket.Conn, original auth.NativeLease, start func(*Client)) {
	principal := original.Principal()
	c := newClient(h, conn, principal.User(), principal.TokenVersion())
	c.native = newNativeSocketState(a, original)
	if ok, err := h.registerPending(c); !ok {
		if err != nil {
			slog.Debug("native socket register pending rejected", "err", err)
		}
		c.shutdown()
		c.close()
		return
	}
	if !h.verifyAndRegisterNative(ctx, a, c, original) {
		h.cancelPending(c)
		return
	}
	start(c)
}

// nativeSocketHandler admits a native connection (rate limit, handshake
// checks, lease authentication, upgrade, pending registration, lease
// revalidation) and then hands the verified client to start, which launches
// its pumps.
func (h *Hub) nativeSocketHandler(a NativeAuthenticator, start func(*Client)) http.Handler {
	connectionsPerIP := httpx.NewRateLimiter(30, time.Minute)
	upgrader := websocket.Upgrader{ReadBufferSize: 4096, WriteBufferSize: 4096, CheckOrigin: nativeHandshakeAllowed}
	core := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if !h.nativeRequestAcceptable(a, w, r) {
			return
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
		h.admitNativeConn(r.Context(), a, conn, original, start)
	})
	limited := connectionsPerIP.PerIP(core)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		limited.ServeHTTP(w, r)
	})
}

// nativeRequestAcceptable rejects, with the matching error, a handshake that
// is not a bare native upgrade; it reports whether the request may proceed.
func (h *Hub) nativeRequestAcceptable(a NativeAuthenticator, w http.ResponseWriter, r *http.Request) bool {
	if a == nil || h.isClosed() {
		httpx.WriteError(w, httpx.ErrUnavailable("native socket unavailable"))
		return false
	}
	if !nativeHandshakeAllowed(r) {
		httpx.WriteError(w, httpx.ErrForbidden("native transport request rejected"))
		return false
	}
	if r.Body != nil {
		var one [1]byte
		if n, err := r.Body.Read(one[:]); n != 0 || err != io.EOF {
			httpx.WriteError(w, httpx.ErrInvalidInput("native socket body must be empty"))
			return false
		}
	}
	return true
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
