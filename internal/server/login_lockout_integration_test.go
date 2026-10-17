//go:build integration

package server

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/msch128/mnema-talk/internal/auth"
)

type lockedUser struct {
	Username    string `json:"username"`
	LoginLocked bool   `json:"login_locked"`
}

func lockedFlags(t *testing.T, admin *client) map[string]bool {
	t.Helper()
	res := admin.get("/api/admin/users")
	if res.status != http.StatusOK {
		t.Fatalf("list users: %d %s", res.status, res.body)
	}
	var users []lockedUser
	res.decode(t, &users)
	out := map[string]bool{}
	for _, u := range users {
		out[u.Username] = u.LoginLocked
	}
	return out
}

func lockOut(t *testing.T, a *app, username string) {
	t.Helper()
	for i := 0; i < 10; i++ {
		a.anon().post("/api/auth/login", map[string]string{"username": username, "password": "wrong-password-xx"})
	}
	if res := a.anon().post("/api/auth/login", map[string]string{"username": username, "password": "member-password-123"}); res.status != http.StatusTooManyRequests {
		t.Fatalf("expected lockout, got %d %s", res.status, res.body)
	}
}

func TestOwnerClearsLoginLockout(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	max := a.register(admin, "max")
	lockOut(t, a, "max")

	if !lockedFlags(t, admin)["max"] {
		t.Fatal("lockout indicator not set")
	}
	if res := max.post("/api/admin/users/"+max.user.ID.String()+"/login-lockout/clear", nil); res.status != http.StatusForbidden {
		t.Fatalf("member clearing a lockout: %d", res.status)
	}
	if res := admin.post("/api/admin/users/00000000-0000-0000-0000-000000000001/login-lockout/clear", nil); res.status != http.StatusNotFound {
		t.Fatalf("unknown user: %d", res.status)
	}
	if res := admin.post("/api/admin/users/"+max.user.ID.String()+"/login-lockout/clear", nil); res.status != http.StatusNoContent {
		t.Fatalf("clear: %d %s", res.status, res.body)
	}
	if lockedFlags(t, admin)["max"] {
		t.Fatal("lockout indicator still set")
	}
	if res := a.anon().post("/api/auth/login", map[string]string{"username": "max", "password": "member-password-123"}); res.status != http.StatusOK {
		t.Fatalf("login after clear: %d %s", res.status, res.body)
	}
}

func TestUnlockRequestFromCLIIsApplied(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	a.register(admin, "max")
	lockOut(t, a, "max")
	ctx := context.Background()

	if _, err := auth.RequestUnlock(ctx, a.db, "nobody", false); err != auth.ErrUnlockUnknownUser {
		t.Fatalf("unknown user: %v", err)
	}
	id, err := auth.RequestUnlock(ctx, a.db, "MAX", false)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		pending, err := auth.UnlockRequestPending(ctx, a.db, id)
		if err != nil {
			t.Fatal(err)
		}
		if !pending {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("server did not pick up the unlock request")
		}
		time.Sleep(100 * time.Millisecond)
	}
	if res := a.anon().post("/api/auth/login", map[string]string{"username": "max", "password": "member-password-123"}); res.status != http.StatusOK {
		t.Fatalf("login after CLI unlock: %d %s", res.status, res.body)
	}

	// --all
	lockOut(t, a, "max")
	id, err = auth.RequestUnlock(ctx, a.db, "", true)
	if err != nil {
		t.Fatal(err)
	}
	for time.Now().Before(deadline.Add(10 * time.Second)) {
		if pending, _ := auth.UnlockRequestPending(ctx, a.db, id); !pending {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if res := a.anon().post("/api/auth/login", map[string]string{"username": "max", "password": "member-password-123"}); res.status != http.StatusOK {
		t.Fatalf("login after unlock --all: %d %s", res.status, res.body)
	}
}
