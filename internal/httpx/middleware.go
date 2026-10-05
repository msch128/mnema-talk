package httpx

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"runtime/debug"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
)

type ctxKey int

const (
	requestIDKey ctxKey = iota
	clientIPKey
)

// requestIDPattern bounds an inbound X-Request-Id we are willing to reflect
// and log; anything else (overlong, control chars, injection attempts) is
// replaced by a fresh UUID.
var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

// RequestID resolves the request id and stores it in the response header and context.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if !requestIDPattern.MatchString(id) {
			id = uuid.NewString()
		}
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey, id)))
	})
}

// RequestIDFromContext returns the id set by RequestID, or "".
func RequestIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey).(string)
	return id
}

const (
	prodStackLimit    = 512
	nonProdStackLimit = 8192
)

// Recover catches panics, logs a bounded stack and answers a JSON 500.
func Recover(prod bool) func(http.Handler) http.Handler {
	limit := nonProdStackLimit
	if prod {
		limit = prodStackLimit
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					if rec == http.ErrAbortHandler {
						panic(rec)
					}
					stack := debug.Stack()
					if len(stack) > limit {
						stack = stack[:limit]
					}
					slog.Error("panic", "request_id", RequestIDFromContext(r.Context()), "err", rec, "stack", string(stack))
					WriteError(w, ErrInternal("internal server error"))
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// Logger emits one structured line per request. Only the path is logged, never
// the query string, so nothing passed in a URL ends up in the logs.
func Logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		if r.URL.Path == "/api/health" {
			return
		}
		slog.Info("http",
			"request_id", RequestIDFromContext(r.Context()),
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.Status(),
			"bytes", ww.BytesWritten(),
			"duration_ms", time.Since(start).Milliseconds(),
			"ip", ClientIP(r),
		)
	})
}

// MaxBody caps request bodies at limit bytes, rejecting a declared oversize
// Content-Length before any byte is read.
func MaxBody(limit int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > limit {
				WriteError(w, ErrPayloadTooLarge("request body too large"))
				return
			}
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, limit)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// SPAContentSecurityPolicy guards the embedded Vue app. No inline scripts and
// no JS eval; 'wasm-unsafe-eval' only allows compiling WebAssembly, which the
// bundled AI noise filter (web/src/lib/noiseSuppressor.js) needs. Inline
// styles are needed for Vue :style bindings. connect-src 'self' also
// covers same-origin WebSockets in current browsers.
const SPAContentSecurityPolicy = "default-src 'self'; " +
	"script-src 'self' 'wasm-unsafe-eval'; " +
	"style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data: blob:; " +
	"media-src 'self' blob:; " +
	"font-src 'self' data:; " +
	"connect-src 'self'; " +
	"worker-src 'self' blob:; " +
	"frame-ancestors 'none'; " +
	"base-uri 'none'; " +
	"form-action 'self'; " +
	"object-src 'none'"

// SecurityHeaders sets defense-in-depth headers on every response. API routes
// get a deny-all CSP and no-store; the SPA gets SPAContentSecurityPolicy.
// HSTS is sent in production, over direct TLS, or when a trusted proxy reports
// X-Forwarded-Proto=https (a spoofed header from an untrusted peer is ignored).
func SecurityHeaders(trusted []*net.IPNet, prod bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "no-referrer")
			h.Set("Cross-Origin-Opener-Policy", "same-origin")
			h.Set("Cross-Origin-Resource-Policy", "same-origin")
			// Voice chat and screen sharing need the microphone and display capture.
			h.Set("Permissions-Policy", "camera=(self), geolocation=(), payment=(), usb=(), microphone=(self), display-capture=(self)")
			if strings.HasPrefix(r.URL.Path, "/api/") {
				h.Set("Cache-Control", "no-store")
				h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
			} else {
				h.Set("Content-Security-Policy", SPAContentSecurityPolicy)
			}
			if prod || r.TLS != nil || forwardedProtoHTTPS(r, trusted) {
				h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
			}
			next.ServeHTTP(w, r)
		})
	}
}

func forwardedProtoHTTPS(r *http.Request, trusted []*net.IPNet) bool {
	if r.Header.Get("X-Forwarded-Proto") != "https" {
		return false
	}
	return remoteTrusted(peerHost(r), trusted)
}

// ParseCIDRs parses CIDRs or bare IPs into networks.
func ParseCIDRs(values []string) ([]*net.IPNet, error) {
	var out []*net.IPNet
	for _, v := range values {
		if !strings.Contains(v, "/") {
			if strings.Contains(v, ":") {
				v += "/128"
			} else {
				v += "/32"
			}
		}
		_, n, err := net.ParseCIDR(v)
		if err != nil {
			return nil, fmt.Errorf("invalid CIDR %q: %w", v, err)
		}
		out = append(out, n)
	}
	return out, nil
}

func peerHost(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// remoteTrusted reports whether host may set forwarding headers: loopback
// always, otherwise only peers inside a configured trusted-proxy CIDR.
func remoteTrusted(host string, trusted []*net.IPNet) bool {
	addr, err := netip.ParseAddr(strings.Trim(host, "[]"))
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	if addr.IsLoopback() {
		return true
	}
	ip := net.IP(addr.AsSlice())
	for _, n := range trusted {
		if n != nil && n.Contains(ip) {
			return true
		}
	}
	return false
}

// ClientIPMiddleware resolves the client IP once per request. X-Forwarded-For
// is only consulted when the direct peer is a trusted proxy, and then the
// right-most hop that is not itself a trusted proxy wins: entries left of it
// were supplied by the client and cannot be trusted for rate limiting.
func ClientIPMiddleware(trusted []*net.IPNet) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			client := peerHost(r)
			if remoteTrusted(client, trusted) {
				hops := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
				for i := len(hops) - 1; i >= 0; i-- {
					addr, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
					if err != nil {
						break
					}
					client = addr.Unmap().String()
					if !remoteTrusted(client, trusted) {
						break
					}
				}
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), clientIPKey, client)))
		})
	}
}

// ClientIP returns the IP resolved by ClientIPMiddleware (or the raw peer).
func ClientIP(r *http.Request) string {
	if ip, ok := r.Context().Value(clientIPKey).(string); ok {
		return ip
	}
	return peerHost(r)
}

// normalizeOrigin reduces an Origin/Referer/allowlist URL to scheme://host.
// CORS and CheckSameOrigin share it so they can never disagree.
func normalizeOrigin(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host)
}

// CheckSameOrigin reports whether the request comes from an allowed origin
// (Origin header, falling back to Referer). Empty or unusable = rejected.
func CheckSameOrigin(r *http.Request, allowedOrigins []string) bool {
	origin := normalizeOrigin(r.Header.Get("Origin"))
	if origin == "" {
		origin = normalizeOrigin(r.Header.Get("Referer"))
	}
	if origin == "" {
		return false
	}
	for _, allowed := range allowedOrigins {
		if origin == normalizeOrigin(allowed) {
			return true
		}
	}
	return false
}

// CSRF rejects state-changing requests that fail CheckSameOrigin. Together with
// the SameSite=Lax session cookie this closes cross-site request forgery on the
// cookie-authenticated API.
func CSRF(allowedOrigins []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
			default:
				if !CheckSameOrigin(r, allowedOrigins) {
					WriteError(w, ErrForbidden("cross-origin request rejected"))
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// CORSAllowedMethods must cover every method the router mounts.
const CORSAllowedMethods = "GET,POST,PUT,PATCH,DELETE,OPTIONS"

// CORS answers allowlisted origins, echoing the allowlist entry rather than the
// raw Origin header, and rejects foreign preflights with 403.
func CORS(allowedOrigins []string) func(http.Handler) http.Handler {
	allowed := make(map[string]string, len(allowedOrigins))
	for _, o := range allowedOrigins {
		if n := normalizeOrigin(o); n != "" {
			allowed[n] = n
		} else {
			slog.Warn("CORS: ignoring origin with no scheme/host", "origin", o)
		}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Add("Vary", "Origin")
			if r.Method == http.MethodOptions {
				w.Header().Add("Vary", "Access-Control-Request-Method")
				w.Header().Add("Vary", "Access-Control-Request-Headers")
			}
			rawOrigin := r.Header.Get("Origin")
			safeOrigin, ok := allowed[normalizeOrigin(rawOrigin)]
			if ok {
				w.Header().Set("Access-Control-Allow-Origin", safeOrigin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type,X-Request-Id")
				w.Header().Set("Access-Control-Allow-Methods", CORSAllowedMethods)
				w.Header().Set("Access-Control-Max-Age", "86400")
			}
			if r.Method == http.MethodOptions {
				if rawOrigin != "" && !ok {
					w.WriteHeader(http.StatusForbidden)
					return
				}
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
