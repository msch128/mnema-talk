//go:build integration

package communityevents

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/testutil"
)

func TestMain(m *testing.M) { os.Exit(testutil.Main(m)) }

func TestTheTalkLookupReportsMissingChannelsAsInputAndDatabaseErrorsAsErrors(t *testing.T) {
	pool := testutil.DB(t)
	r := httptest.NewRequest(http.MethodPost, "/api/events", nil)

	var apiErr *httpx.APIError
	h := &Handler{DB: pool}
	if err := h.validateEventLocation(r, ptr(uuid.New()), nil); !errors.As(err, &apiErr) || apiErr.Status != http.StatusBadRequest {
		t.Fatalf("an unknown Talk is invalid input, got %v", err)
	}

	broken, err := pgxpool.NewWithConfig(context.Background(), pool.Config())
	if err != nil {
		t.Fatal(err)
	}
	broken.Close()
	h = &Handler{DB: &db.Pool{Pool: broken}}
	err = h.validateEventLocation(r, ptr(uuid.New()), nil)
	if err == nil || errors.As(err, &apiErr) {
		t.Fatalf("a database failure must not read as invalid input, got %v", err)
	}
}

func ptr[T any](v T) *T { return &v }
