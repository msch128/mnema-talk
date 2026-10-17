//go:build integration

package server

import (
	"context"
	"net/http"
	"testing"

	"github.com/msch128/mnema-talk/internal/auth"
)

func TestLoginLockoutEdges(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	if res := admin.post("/api/admin/users/not-a-uuid/login-lockout/clear", nil); res.status != http.StatusBadRequest {
		t.Fatalf("bad id: %d", res.status)
	}

	h := auth.NewHandler(&auth.Sessions{DB: a.db}, a.events)
	ctx := context.Background()

	// A queued request for an account that no longer exists still applies.
	if _, err := a.db.Exec(ctx, `INSERT INTO login_unlock_requests (username) VALUES ('ghost')`); err != nil {
		t.Fatal(err)
	}
	if n, err := h.ProcessUnlockRequests(ctx); err != nil || n != 1 {
		t.Fatalf("process = %d, %v", n, err)
	}

	// A cancelled context surfaces database errors instead of hiding them.
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := h.ProcessUnlockRequests(cancelled); err == nil {
		t.Fatal("process ignored the cancelled context")
	}
	if _, err := auth.RequestUnlock(cancelled, a.db, "x", false); err == nil {
		t.Fatal("request ignored the cancelled context")
	}
	if _, err := auth.RequestUnlock(cancelled, a.db, "", true); err == nil {
		t.Fatal("request --all ignored the cancelled context")
	}
	// The worker stops with its context.
	h.RunUnlockWorker(cancelled, auth.UnlockPollInterval)
}
