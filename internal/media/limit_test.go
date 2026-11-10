package media

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/perm/permtest"
)

func TestUploadLimitResolution(t *testing.T) {
	ctx := context.Background()
	h := &Handler{MaxUploadBytes: 4 << 20}
	if got := h.limit(ctx); got != 4<<20 {
		t.Fatalf("no context limit: %d", got)
	}
	if got := h.limit(WithUploadLimit(ctx, 1<<20)); got != 1<<20 {
		t.Fatalf("lower request limit: %d", got)
	}
	// A request limit can never exceed the operator's ceiling.
	if got := h.limit(WithUploadLimit(ctx, 9<<20)); got != 4<<20 {
		t.Fatalf("limit above the ceiling: %d", got)
	}

	// Without the permission model nobody may upload anything.
	if got, err := h.memberLimit(ctx, perm.Subject{ID: uuid.New(), Owner: true}); err == nil || got != 0 {
		t.Fatalf("member limit without perms: %d %v", got, err)
	}
	if _, _, err := h.GlobalLimit(ctx); err == nil {
		t.Fatal("global limit without perms must fail")
	}

	// A permission state that cannot be read fails the request closed.
	h.Perms = permtest.Unloadable()
	if _, err := h.memberLimit(ctx, perm.Subject{ID: uuid.New()}); err == nil {
		t.Fatal("member limit must fail without a readable state")
	}
	if _, _, err := h.GlobalLimit(ctx); err == nil {
		t.Fatal("global limit must fail without a readable state")
	}
}
