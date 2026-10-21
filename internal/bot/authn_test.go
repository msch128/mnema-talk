package bot

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestBearerToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if _, ok := BearerToken(req); ok {
		t.Fatalf("expected no bearer token for empty header")
	}

	req.Header.Set("Authorization", "Bearer xyz")
	if _, ok := BearerToken(req); ok {
		t.Fatalf("expected no bot token for Bearer scheme")
	}

	req.Header.Set("Authorization", "Bot my-test-token")
	token, ok := BearerToken(req)
	if !ok || token != "my-test-token" {
		t.Fatalf("expected my-test-token, got %s (ok=%v)", token, ok)
	}

	req.Header.Set("Authorization", "bot   spaced-token  ")
	token, ok = BearerToken(req)
	if !ok || token != "spaced-token" {
		t.Fatalf("expected spaced-token, got %s", token)
	}
}

func TestShouldTouch(t *testing.T) {
	a := NewAuthenticator(nil)
	id := uuid.New()
	now := time.Now()

	// Initial touch with no previous timestamp should succeed
	if !a.shouldTouch(id, nil, now) {
		t.Errorf("expected initial touch to succeed")
	}

	// Repeated touch within touchEvery should be throttled
	if a.shouldTouch(id, nil, now.Add(10*time.Second)) {
		t.Errorf("expected recent touch to be throttled")
	}

	// Touch after touchEvery should succeed
	if !a.shouldTouch(id, nil, now.Add(touchEvery+time.Second)) {
		t.Errorf("expected touch after window to succeed")
	}

	// The touch just granted restarts the window.
	if a.shouldTouch(id, nil, now.Add(touchEvery+2*time.Second)) {
		t.Errorf("expected the call right after a granted touch to be throttled")
	}

	// Unseen id with recent db last timestamp should be throttled
	otherID := uuid.New()
	last := now.Add(-10 * time.Second)
	if a.shouldTouch(otherID, &last, now) {
		t.Errorf("expected unseen id with recent db last-used to be throttled")
	}

	// Unseen id with old db last timestamp should succeed
	oldLast := now.Add(-2 * touchEvery)
	if !a.shouldTouch(otherID, &oldLast, now) {
		t.Errorf("expected unseen id with old db last-used to succeed")
	}
}
