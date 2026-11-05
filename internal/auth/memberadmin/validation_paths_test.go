package memberadmin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
)

const secret = "0123456789abcdef0123456789abcdef"

func TestMemberAdminHandlersRejectMalformedJSON(t *testing.T) {
	h := New(auth.NewHandler(&auth.Sessions{Secret: secret}, nil))
	u := &auth.User{ID: uuid.New()}
	for name, handler := range map[string]httpx.HandlerFunc{
		"invite": h.createInvite, "admin password": h.setUserPassword,
	} {
		t.Run(name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{"))
			r = r.WithContext(auth.WithUser(r.Context(), u))
			rc := chi.NewRouteContext()
			rc.URLParams.Add("id", uuid.NewString())
			r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rc))
			w := httptest.NewRecorder()
			httpx.Handle(handler).ServeHTTP(w, r)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestMemberAdminHandlersRejectMalformedIDs(t *testing.T) {
	h := New(auth.NewHandler(&auth.Sessions{}, nil))
	for name, handler := range map[string]httpx.HandlerFunc{
		"delete invite": h.deleteInvite, "disable": h.disableUser,
		"enable": h.enableUser, "revoke": h.revokeUserSessions, "reset": h.resetUserPassword,
		"password": h.setUserPassword, "kick": h.kickUser, "status": h.setUserStatus,
	} {
		t.Run(name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/", nil)
			rc := chi.NewRouteContext()
			rc.URLParams.Add("id", "not-a-uuid")
			rc.URLParams.Add("userID", "not-a-uuid")
			r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rc))
			w := httptest.NewRecorder()
			httpx.Handle(handler).ServeHTTP(w, r)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d", w.Code)
			}
		})
	}
}
