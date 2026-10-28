package httpx

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/msch128/mnema-talk/internal/metrics"
)

func isWebSocketUpgrade(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
}

// Metrics counts every request by route pattern (never the raw path, so ids
// in URLs cannot multiply the series) and status class, and times it.
// Upgraded WebSocket connections are counted but not timed: they live for
// hours.
func Metrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		metrics.HTTPInflight.Add(1)
		defer func() {
			metrics.HTTPInflight.Add(-1)
			route := "unmatched"
			if rc := chi.RouteContext(r.Context()); rc != nil && rc.RoutePattern() != "" {
				route = rc.RoutePattern()
			}
			status := ww.Status()
			if status == 0 {
				status = http.StatusOK // nothing written: net/http answers 200
			}
			// gorilla/websocket hijacks the connection and writes the 101 itself,
			// so the wrapper never sees WriteHeader: the Upgrade header tells.
			if status == http.StatusSwitchingProtocols || (ww.Status() == 0 && isWebSocketUpgrade(r)) {
				metrics.HTTPRequests.With(route, "1xx").Inc()
				return
			}
			metrics.ObserveHTTP(route, status, time.Since(start))
		}()
		next.ServeHTTP(ww, r)
	})
}
