package authn

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
	"github.com/msch128/mnema-talk/internal/perm/permtest"
)

func TestAuthHandlersRejectMalformedJSON(t *testing.T) {
	h := NewHandler(&auth.Sessions{Secret: secret}, permtest.NewFixture().Service(), nil)
	u := &auth.User{ID: uuid.New()}
	for name, handler := range map[string]httpx.HandlerFunc{
		"login": h.login, "register": h.register, "password": h.changePassword,
		"locale": h.setLocale, "profile": h.updateProfile, "presence": h.setPresence,
		"status": h.setStatus,
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

func TestProfileHandlerRejectsMalformedIDs(t *testing.T) {
	h := NewHandler(&auth.Sessions{}, permtest.NewFixture().Service(), nil)
	for name, handler := range map[string]httpx.HandlerFunc{
		"get": h.getUser,
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
