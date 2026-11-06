//go:build integration

package authn

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
)

func TestAuthHandlersPropagateDatabaseCancellation(t *testing.T) {
	p, admin, member := authFixture(t)
	for _, tc := range []struct {
		name, query, body string
		handler           func(*Handler) httpx.HandlerFunc
		id                uuid.UUID
	}{
		{"logout all", "UPDATE users SET token_version", "", func(h *Handler) httpx.HandlerFunc { return h.logoutAll }, member.ID},
		{"locale", "UPDATE users SET locale", `{"locale":"en"}`, func(h *Handler) httpx.HandlerFunc { return h.setLocale }, member.ID},
		{"profile", "UPDATE users SET display_name", `{"display_name":"new"}`, func(h *Handler) httpx.HandlerFunc { return h.updateProfile }, member.ID},
		{"presence", "UPDATE users SET presence", `{"presence":"away"}`, func(h *Handler) httpx.HandlerFunc { return h.setPresence }, member.ID},
		{"status", "UPDATE users SET status_text", `{"status_text":"busy"}`, func(h *Handler) httpx.HandlerFunc { return h.setStatus }, member.ID},
		{"profile load", "FROM users WHERE id = $1", "", func(h *Handler) httpx.HandlerFunc { return h.getUser }, member.ID},
		{"profile stats", "SELECT public_voice_seconds", "", func(h *Handler) httpx.HandlerFunc { return h.getUser }, member.ID},
		{"confirm password", "SELECT password_hash FROM users", "", nil, member.ID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fault := authHookPool(t, p, &authQueryHook{match: tc.query, cancel: true})
			h := NewHandler(&auth.Sessions{DB: fault, Secret: secret, TTL: time.Hour}, &events.Recorder{})
			if tc.handler == nil {
				if wait, err := h.ConfirmPassword(context.Background(), member.ID, testPassword); wait != 0 || !errors.Is(err, context.Canceled) {
					t.Fatalf("confirmation = %v, %v", wait, err)
				}
				return
			}
			w := authRequest(t, tc.handler(h), admin, tc.body, tc.id)
			if w.Code != http.StatusInternalServerError {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if len(w.Result().Cookies()) != 0 {
				t.Fatal("failed operation changed session cookie")
			}
		})
	}
}

// Failed validation must return an API error and retain the current cookie.
func TestChangePasswordValidationKeepsTheCookie(t *testing.T) {
	p, _, member := authFixture(t)
	h := NewHandler(&auth.Sessions{DB: p}, &events.Recorder{})
	w := authRequest(t, h.changePassword, member, `{"current_password":"unused","new_password":"short"}`, member.ID)
	if w.Code != 400 || len(w.Result().Cookies()) != 0 {
		t.Fatalf("password validation status=%d cookies=%d", w.Code, len(w.Result().Cookies()))
	}
}

func TestUpdateDisplayNameKeepsTheStoredBio(t *testing.T) {
	p, _, member := authFixture(t)
	ctx := context.Background()
	if _, err := auth.UpdateProfile(ctx, p, member.ID, "before", "my bio"); err != nil {
		t.Fatal(err)
	}
	u, err := auth.UpdateDisplayName(ctx, p, member.ID, "after")
	if err != nil {
		t.Fatal(err)
	}
	if u.DisplayName != "after" || u.Bio != "my bio" {
		t.Fatalf("display name %q, bio %q", u.DisplayName, u.Bio)
	}
	if _, err := auth.UpdateDisplayName(ctx, p, uuid.New(), "x"); err == nil {
		t.Fatal("unknown user accepted")
	}
}
