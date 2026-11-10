package messages

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/perm"
)

func TestModerationNeedsThePermissionService(t *testing.T) {
	h := &API{}
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r = r.WithContext(auth.WithUser(r.Context(), &auth.User{ID: uuid.New(), Role: "admin"}))
	err := h.asModerator(r, uuid.New(), func(pgx.Tx, *perm.Snapshot) error {
		t.Fatal("ran without a permission check")
		return nil
	})
	if err == nil {
		t.Fatal("moderated without a permission service")
	}
}
