//go:build integration

package authn

import (
	"context"
	"errors"
	"testing"

	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/perm/permtest"
)

// A sweep that cannot read the database reports that, and does not pretend
// the status lines were cleared.
func TestStatusExpiryReportsWhenTheClearCannotBeQueried(t *testing.T) {
	pool, _, _ := authFixture(t)
	h := NewHandler(&auth.Sessions{DB: pool, Secret: "status-expiry-test-secret-value"}, permtest.NewFixture().Service(), nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := h.ExpireStatuses(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want the cancelled query", err)
	}
}
