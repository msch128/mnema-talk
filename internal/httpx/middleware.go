package httpx

import (
	"context"
	"log/slog"
	"net"
	"net/http"
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
	prodStackLimit    = 2048
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
					slog.Error("panic", "request_id", RequestIDFromContext(r.Context()), "err", rec, "stack", panicStack(debug.Stack(), limit))
					WriteError(w, ErrInternal("internal server error"))
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// panicStack drops the goroutine header and the leading frames that only
// describe the recovery (debug.Stack, the deferred closure, panic and the
// runtime's own panic helpers), so the limit cuts off the tail of a deep stack
// and never the frame that panicked.
func panicStack(stack []byte, limit int) string {
	lines := strings.Split(string(stack), "\n")
	start := 0
	for i := 0; i+1 < len(lines); i++ {
		if strings.HasPrefix(lines[i], "panic(") {
			start = i + 2
			for start+1 < len(lines) && strings.HasPrefix(lines[start], "runtime.") {
				start += 2
			}
			break
		}
	}
	out := strings.Join(lines[start:], "\n")
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// secretPathPrefixes name the routes that carry a secret as the path segment
// after the next one: /api/webhooks/{id}/{token} and
// /api/bot/v1/interactions/{interactionID}/{token}/...
var secretPathPrefixes = []string{"/api/webhooks/", "/api/bot/v1/interactions/"}

// LogPath is the path as it may appear in logs: the secret token of a webhook
// execute URL (/api/webhooks/{id}/{token}) and of a bot interaction response
// URL (/api/bot/v1/interactions/{id}/{token}/callback) is replaced.
func LogPath(path string) string {
	for _, prefix := range secretPathPrefixes {
		rest, ok := strings.CutPrefix(path, prefix)
		if !ok {
			continue
		}
		id, afterID, found := strings.Cut(rest, "/")
		if !found {
			return path
		}
		token, tail, _ := strings.Cut(afterID, "/")
		if token == "" {
			return path
		}
		if prefix == "/api/webhooks/" {
			tail = "" // a webhook URL is nothing but id and token
		} else if tail != "" {
			tail = "/" + tail
		}
		return prefix + id + "/[redacted]" + tail
	}
	return path
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
			"path", LogPath(r.URL.Path),
			"status", ww.Status(),
			"bytes", ww.BytesWritten(),
			"duration_ms", time.Since(start).Milliseconds(),
			"ip", ClientIP(r),
		)
	})
}

// jsonReadTimeout is how long a client gets to send a small (JSON) request
// body. The server-wide ReadTimeout is long for uploads and would otherwise
// let a slow sender hold a connection for ten minutes.
var jsonReadTimeout = 30 * time.Second

// SetJSONReadDeadline shortens the read deadline of this request to
// jsonReadTimeout. Servers without deadline support (tests) are unaffected.
func SetJSONReadDeadline(w http.ResponseWriter) {
	_ = http.NewResponseController(w).SetReadDeadline(time.Now().Add(jsonReadTimeout))
}

// MaxBody caps request bodies at limit bytes, rejecting a declared oversize
// Content-Length before any byte is read.
func MaxBody(limit int64) func(http.Handler) http.Handler { return maxBody(limit, WriteError) }

// MaxBodyDiscord is MaxBody with the bot API's Discord-shaped error.
func MaxBodyDiscord(limit int64) func(http.Handler) http.Handler {
	return maxBody(limit, WriteDiscordError)
}

func maxBody(limit int64, refuse func(http.ResponseWriter, error)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > limit {
				refuse(w, ErrPayloadTooLarge("request body too large"))
				return
			}
			if limit <= DefaultMaxBody {
				SetJSONReadDeadline(w)
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
// bundled AI noise filter (web/src/lib/noiseSuppressor.js) needs.
//
// style-src is 'self' with style-src-attr 'none': no <style> elements and no
// style="" attributes, only stylesheet files and constructable stylesheets.
// The app's own code needs nothing else: Vue :style bindings go through the
// CSSOM, which style-src does not restrict, and SFC styles are extracted to
// files. The one library that wrote inline styles, emoji-picker-element, is
// patched at build time to use adoptedStyleSheets and the CSSOM
// (web/scripts/emojiPickerCsp.ts). The Playwright fixtures fail every spec on
// any securitypolicyviolation event, so a regression cannot ship unnoticed.
// frame-src names the two video players a link card can load after a click:
// youtube-nocookie.com (YouTube's privacy-enhanced host) and Vimeo's player.
// Nothing else may be framed, and the card sandboxes the iframe. See
// SECURITY.md, "Link previews and video playback".
// connect-src 'self' also covers same-origin WebSockets in current browsers.
//
// The Trusted Types directives are a second line behind the escaping of the
// Markdown renderer: with them a plain string at an HTML or script-URL sink
// (innerHTML, new Worker, serviceWorker.register, ...) throws instead of being
// parsed, and only the policies named here can make a value a sink accepts.
// A browser without Trusted Types ignores both directives.
const SPAContentSecurityPolicy = basePolicy + "; " + trustedTypesDirectives

// APIDocsContentSecurityPolicy guards the API reference page (/api/docs, only
// with API_DOCS_ENABLED). It is the app's policy without the Trusted Types
// directives: the bundled Swagger UI writes sanitized strings to innerHTML
// (React's dangerouslySetInnerHTML) and has no switch to hand over TrustedHTML.
const APIDocsContentSecurityPolicy = basePolicy

// The policies, one per sink the app has (web/src/lib/trustedTypes.ts):
//   - vue: Vue's own, for the static markup its template compiler hoists and
//     for v-html. It passes text through, so what keeps a second v-html or a
//     bound innerHTML out stays the lint rules (web/eslint.config.ts).
//   - mnema-markdown: the Markdown renderer; its output is the one v-html.
//   - mnema-script-url: the app's own worker scripts, same origin and only
//     /sw.js or a file under the build's asset prefix.
//   - mnema-emoji-picker: the static templates of emoji-picker-element
//     (web/scripts/emojiPickerCsp.ts), created at start-up like the others.
//
// A name missing here, or a second policy of one name, is refused by the browser.
const trustedTypesDirectives = "require-trusted-types-for 'script'; " +
	"trusted-types vue mnema-markdown mnema-script-url mnema-emoji-picker"

const basePolicy = "default-src 'self'; " +
	"script-src 'self' 'wasm-unsafe-eval'; " +
	"style-src 'self'; " +
	"style-src-attr 'none'; " +
	"img-src 'self' data: blob:; " +
	"media-src 'self' blob:; " +
	"font-src 'self' data:; " +
	"connect-src 'self'; " +
	"worker-src 'self' blob:; " +
	"frame-src https://www.youtube-nocookie.com https://player.vimeo.com; " +
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
