//go:build integration

package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
)

func modRequest(t *testing.T, h httpx.HandlerFunc, user *User, body, id string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	r = r.WithContext(WithUser(r.Context(), user))
	rc := chi.NewRouteContext()
	rc.URLParams.Add("id", id)
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rc))
	w := httptest.NewRecorder()
	httpx.Handle(h).ServeHTTP(w, r)
	return w
}

func TestModerationHandlersRejectBadInput(t *testing.T) {
	p, admin, member := authFixture(t)
	h := NewHandler(&Sessions{DB: p, Secret: secret, TTL: time.Hour}, &events.Recorder{})
	for name, tc := range map[string]struct {
		handler func(*Handler) httpx.HandlerFunc
		body    string
		id      string
	}{
		"timeout id":      {func(h *Handler) httpx.HandlerFunc { return h.timeoutUser }, `{"duration_seconds":60}`, "nope"},
		"timeout json":    {func(h *Handler) httpx.HandlerFunc { return h.timeoutUser }, `{`, member.ID.String()},
		"timeout reason":  {func(h *Handler) httpx.HandlerFunc { return h.timeoutUser }, `{"duration_seconds":60,"reason":"` + strings.Repeat("x", 300) + `"}`, member.ID.String()},
		"untimeout id":    {func(h *Handler) httpx.HandlerFunc { return h.untimeoutUser }, ``, "nope"},
		"ban id":          {func(h *Handler) httpx.HandlerFunc { return h.banUser }, `{}`, "nope"},
		"ban json":        {func(h *Handler) httpx.HandlerFunc { return h.banUser }, `{`, member.ID.String()},
		"ban reason":      {func(h *Handler) httpx.HandlerFunc { return h.banUser }, `{"reason":"` + strings.Repeat("x", 300) + `"}`, member.ID.String()},
		"unban id":        {func(h *Handler) httpx.HandlerFunc { return h.unbanUser }, ``, "nope"},
		"timeout unknown": {func(h *Handler) httpx.HandlerFunc { return h.timeoutUser }, `{"duration_seconds":60}`, uuid.NewString()},
	} {
		t.Run(name, func(t *testing.T) {
			if w := modRequest(t, tc.handler(h), admin, tc.body, tc.id); w.Code < 400 || w.Code >= 500 {
				t.Fatalf("status %d %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestModerationPropagatesDatabaseFailures(t *testing.T) {
	p, admin, member := authFixture(t)
	for _, tc := range []struct {
		name, query, body string
		handler           func(*Handler) httpx.HandlerFunc
	}{
		{"timeout write", "UPDATE users SET timeout_until = NOW()", `{"duration_seconds":60}`, func(h *Handler) httpx.HandlerFunc { return h.timeoutUser }},
		{"untimeout write", "UPDATE users SET timeout_until = NULL", ``, func(h *Handler) httpx.HandlerFunc { return h.untimeoutUser }},
		{"ban write", "UPDATE users SET banned_at = NOW()", `{}`, func(h *Handler) httpx.HandlerFunc { return h.banUser }},
		{"ban invite revoke", "DELETE FROM invites WHERE id = (SELECT invite_id", `{}`, func(h *Handler) httpx.HandlerFunc { return h.banUser }},
		{"unban write", "UPDATE users SET banned_at = NULL", ``, func(h *Handler) httpx.HandlerFunc { return h.unbanUser }},
		{"ban list", "WHERE u.banned_at IS NOT NULL", ``, func(h *Handler) httpx.HandlerFunc { return h.listBans }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fault := authHookPool(t, p, &authQueryHook{match: tc.query, cancel: true})
			h := NewHandler(&Sessions{DB: fault, Secret: secret, TTL: time.Hour}, &events.Recorder{})
			w := authRequest(t, tc.handler(h), admin, tc.body, member.ID)
			if w.Code != http.StatusInternalServerError {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestBanExpiryAndBanMessageFailuresAreSurvivable(t *testing.T) {
	p, _, member := authFixture(t)
	ctx := context.Background()

	// A failing expiry run only logs.
	fault := authHookPool(t, p, &authQueryHook{match: "UPDATE users SET banned_at = NULL, banned_until = NULL", cancel: true})
	NewHandler(&Sessions{DB: fault, Secret: secret, TTL: time.Hour}, &events.Recorder{}).ExpireBans(ctx)

	// The ban message needs the ban; a failing lookup is an error, a missing ban is plain "disabled".
	if err := bannedError(ctx, p, member.ID); !errors.Is(err, ErrAccountDisabled) {
		t.Fatalf("no ban: %v", err)
	}
	until := time.Now().Add(time.Hour)
	if err := setBan(ctx, p, member.ID, member.ID, "because", &until, false); err != nil {
		t.Fatal(err)
	}
	err := bannedError(ctx, p, member.ID)
	if ae, ok := httpx.AsAPIError(err); !ok || !strings.Contains(ae.Message, "until") || !strings.Contains(ae.Message, "because") {
		t.Fatalf("ban message: %v", err)
	}
	broken := authHookPool(t, p, &authQueryHook{match: "SELECT ban_reason, banned_until", cancel: true})
	if err := bannedError(ctx, broken, member.ID); err == nil || errors.Is(err, ErrAccountDisabled) {
		t.Fatalf("lookup failure = %v", err)
	}
}
