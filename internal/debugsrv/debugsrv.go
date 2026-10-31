// Package debugsrv serves Go's pprof profiles on a loopback-only address, for
// finding what a loaded server spends its CPU and memory on. It is off unless
// PPROF_ADDR is set, and it refuses any address that is not loopback: profiles
// reveal memory contents and the endpoints are unauthenticated.
package debugsrv

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/pprof"
	"strings"
	"time"
)

// ErrNotLoopback is returned for an address that would be reachable from the network.
var ErrNotLoopback = errors.New("must be a loopback address with a port, for example 127.0.0.1:6060")

// CheckAddr accepts only host:port with an IP loopback host or "localhost".
func CheckAddr(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		return ErrNotLoopback
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	return ErrNotLoopback
}

func handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	return mux
}

// Start serves pprof on addr until ctx ends and returns once it is listening.
// An empty addr starts nothing. The returned address is the one bound.
func Start(ctx context.Context, addr string) (string, error) {
	if addr == "" {
		return "", nil
	}
	if err := CheckAddr(addr); err != nil {
		return "", fmt.Errorf("PPROF_ADDR %q %w", addr, err)
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return "", fmt.Errorf("PPROF_ADDR listen: %w", err)
	}
	if !ln.Addr().(*net.TCPAddr).IP.IsLoopback() {
		_ = ln.Close()
		return "", fmt.Errorf("PPROF_ADDR %q %w", addr, ErrNotLoopback)
	}
	srv := &http.Server{Handler: handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("pprof server stopped", "err", err)
		}
	}()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	slog.Warn("pprof is enabled (PPROF_ADDR): profiles are served without authentication on a loopback address", "addr", ln.Addr().String())
	return ln.Addr().String(), nil
}
