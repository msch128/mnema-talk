//go:build integration

package media

import (
	"context"
	"testing"
)

func TestDashboardRejectsUnrepresentablePostgresTimestamp(t *testing.T) {
	f := mediaDB(t)
	id := f.attachment(t, "uploads/infinite-date", false)
	// PostgreSQL permits infinity for timestamptz; the Go dashboard requires a finite time.Time.
	execMedia(t, f.p, `UPDATE media SET created_at='infinity'::timestamptz WHERE id=$1`, id)
	if items, err := ListMedia(context.Background(), f.p, 10, 0); err == nil || items != nil {
		t.Fatalf("infinite dashboard timestamp: items=%v error=%v", items, err)
	}
}
