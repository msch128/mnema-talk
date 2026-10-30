package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/portconflict"
)

// Run starts the HTTP server and blocks until ctx is cancelled, then shuts
// down gracefully.
func Run(ctx context.Context, cfg *config.Config, handler http.Handler) error {
	srv := &http.Server{
		Addr:              net.JoinHostPort(cfg.BindAddr, cfg.Port),
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		// Generous body/response timeouts: uploads and video downloads can be
		// large; WebSockets set their own deadlines after the upgrade.
		ReadTimeout:    10 * time.Minute,
		WriteTimeout:   10 * time.Minute,
		IdleTimeout:    120 * time.Second,
		MaxHeaderBytes: 64 << 10,
		ErrorLog:       slog.NewLogLogger(slog.Default().Handler(), slog.LevelWarn),
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", srv.Addr, "public_url", cfg.PublicURL)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("http server: %w", portconflict.Wrap(err, cfg.Port, "TCP", "PORT"))
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}
	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
