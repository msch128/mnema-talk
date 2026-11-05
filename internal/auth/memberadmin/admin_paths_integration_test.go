//go:build integration

package memberadmin

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/auth/authn"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
)

func TestMemberAdminHandlersPropagateDatabaseCancellation(t *testing.T) {
	p, admin, member := authFixture(t)
	for _, tc := range []struct {
		name, query, body string
		handler           func(*API) httpx.HandlerFunc
		id                uuid.UUID
	}{
		{"invitation list", "FROM invites ORDER BY", "", func(h *API) httpx.HandlerFunc { return h.listInvites }, member.ID},
		{"admin list", "ORDER BY role = 'admin' DESC", "", func(h *API) httpx.HandlerFunc { return h.listUsers }, member.ID},
		{"disable", "UPDATE users SET disabled_at", "", func(h *API) httpx.HandlerFunc { return h.disableUser }, member.ID},
		{"enable", "UPDATE users SET disabled_at", "", func(h *API) httpx.HandlerFunc { return h.enableUser }, member.ID},
		{"revoke target load", "FROM users WHERE id = $1", "", func(h *API) httpx.HandlerFunc { return h.revokeUserSessions }, member.ID},
		{"kick target load", "FROM users WHERE id = $1", "", func(h *API) httpx.HandlerFunc { return h.kickUser }, member.ID},
		{"revoke write", "UPDATE users SET token_version", "", func(h *API) httpx.HandlerFunc { return h.revokeUserSessions }, member.ID},
		{"admin set password", "UPDATE users SET password_hash", `{"password":"replacement-password","current_password":"` + testPassword + `"}`, func(h *API) httpx.HandlerFunc { return h.setUserPassword }, member.ID},
		{"admin reset password", "UPDATE users SET password_hash", `{"current_password":"` + testPassword + `"}`, func(h *API) httpx.HandlerFunc { return h.resetUserPassword }, member.ID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fault := authHookPool(t, p, &authQueryHook{match: tc.query, cancel: true})
			h := New(authn.NewHandler(&auth.Sessions{DB: fault, Secret: secret, TTL: time.Hour}, &events.Recorder{}))
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

func TestAdminKickRejectsMissingOrDisconnectedUser(t *testing.T) {
	p, admin, member := authFixture(t)
	h := New(authn.NewHandler(&auth.Sessions{DB: p}, &events.Recorder{}))
	for _, tc := range []struct {
		id     uuid.UUID
		status int
	}{{uuid.New(), 404}, {member.ID, 400}} {
		w := authRequest(t, h.kickUser, admin, "", tc.id)
		if w.Code != tc.status {
			t.Fatalf("kick status=%d want%d", w.Code, tc.status)
		}
	}
}
