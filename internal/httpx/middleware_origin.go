package httpx

import (
	"log/slog"
	"net/http"
	"net/url"
	"strings"
)

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
	return CSRFExcept(allowedOrigins, nil)
}

// CSRFExcept is CSRF that lets requests through for which skip returns true.
// Only routes that never read the session cookie may be skipped: they carry
// their own secret, so there is no ambient credential to forge.
func CSRFExcept(allowedOrigins []string, skip func(*http.Request) bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if skip != nil && skip(r) {
				next.ServeHTTP(w, r)
				return
			}
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
