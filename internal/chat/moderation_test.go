package chat

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

func TestModerationNeedsThePermissionService(t *testing.T) {
	h := &Handler{}
	err := h.asModerator(httptest.NewRequest(http.MethodPost, "/", nil), uuid.New(), func(pgx.Tx, *perm.Snapshot) error {
		t.Fatal("ran without a permission check")
		return nil
	})
	if ae, ok := httpx.AsAPIError(err); !ok || ae.Status != http.StatusForbidden {
		t.Fatalf("err = %v", err)
	}
}
