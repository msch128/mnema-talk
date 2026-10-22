package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/msch128/mnema-talk/internal/httpx"
)

// fillSlots occupies every hashing slot until the test ends.
func fillSlots(t *testing.T) {
	t.Helper()
	n := cap(bcryptSlots)
	for i := 0; i < n; i++ {
		bcryptSlots <- struct{}{}
	}
	t.Cleanup(func() {
		for i := 0; i < n; i++ {
			<-bcryptSlots
		}
	})
}

func TestSaturatedHashingQueueFailsFastInsteadOfBlocking(t *testing.T) {
	fillSlots(t)
	old := bcryptMaxWait
	bcryptMaxWait = 50 * time.Millisecond
	t.Cleanup(func() { bcryptMaxWait = old })

	start := time.Now()
	if err := burnPasswordCheck(context.Background(), "x"); !errors.Is(err, ErrServerBusy) {
		t.Fatalf("got %v, want ErrServerBusy", err)
	}
	if time.Since(start) > time.Second {
		t.Fatalf("waited %v, want about the 50ms limit", time.Since(start))
	}
}

func TestQueuedHashingGivesUpWhenClientDisconnects(t *testing.T) {
	fillSlots(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := checkPasswordCtx(ctx, "x", "hash"); done <- err }()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("a cancelled request kept waiting for a hashing slot")
	}
}

func TestHashingQueueDepthIsBounded(t *testing.T) {
	fillSlots(t)
	old := bcryptMaxQueue
	bcryptMaxQueue = 0
	t.Cleanup(func() { bcryptMaxQueue = old })
	if _, err := hashPasswordCtx(context.Background(), "x"); !errors.Is(err, ErrServerBusy) {
		t.Fatalf("got %v, want ErrServerBusy at once", err)
	}
	if bcryptWaiting.Load() != 0 {
		t.Fatalf("waiting counter leaked: %d", bcryptWaiting.Load())
	}
}

func TestLoginAnswers503WithRetryAfterWhenBusy(t *testing.T) {
	h := newLoginHandler()
	h.verify = func(context.Context, string, string) (*User, int, error) { return nil, 0, ErrServerBusy }
	rec := postLoginWith(h, "203.0.113.5", "Herzog", "whatever-password")
	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("got %d retry-after %q, want 503 with Retry-After", rec.Code, rec.Header().Get("Retry-After"))
	}
	if locked, _ := h.loginFailures.IsLockedOut("203.0.113.5|herzog"); locked || h.loginFailures.Len() != 0 {
		t.Fatal("a busy server must not count as a failed login")
	}
}

func TestImplausibleUsernameDoesNoHashing(t *testing.T) {
	fillSlots(t)
	h := newLoginHandler()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"has space","password":"x"}`))
	done := make(chan struct{})
	go func() { httpx.Handle(h.login).ServeHTTP(rec, req); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("an impossible username waited for a hashing slot")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401", rec.Code)
	}
}
