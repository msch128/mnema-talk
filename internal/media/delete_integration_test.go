//go:build integration

package media

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestDeleteMediaFailuresAndIdempotence(t *testing.T) {
	f := mediaDB(t)
	id := f.attachment(t, "uploads/a", false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := DeleteMedia(ctx, f.p, f.store, id); err == nil || !strings.Contains(err.Error(), "load media") {
		t.Fatalf("canceled delete=%v", err)
	}
	requireAPIError(t, DeleteMedia(context.Background(), f.p, f.store, uuid.New()), 404)
	f.store.deleteErr = errors.New("storage refused delete")
	if err := DeleteMedia(context.Background(), f.p, f.store, id); err == nil || !strings.Contains(err.Error(), "delete object") {
		t.Fatalf("object failure=%v", err)
	}
	f.store.deleteErr = nil
	if err := DeleteMedia(context.Background(), f.p, f.store, id); err != nil {
		t.Fatal(err)
	}
	if err := DeleteMedia(context.Background(), f.p, f.store, id); err != nil {
		t.Fatal(err)
	}
	if f.store.Has("uploads/a") {
		t.Fatal("deleted object remains")
	}
}
