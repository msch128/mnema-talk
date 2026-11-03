//go:build integration

package push

import (
	"context"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/metrics"
	"github.com/msch128/mnema-talk/internal/worker"
)

func TestTheSweepDropsDevicesWhoseSessionHasEnded(t *testing.T) {
	e := newEnv(t, http.StatusCreated)
	bob := e.member("bob")
	e.exec(`UPDATE push_subscriptions SET session_expires_at = NOW() - INTERVAL '1 minute' WHERE user_id = $1`, bob.id)
	// The env's delivery loop initialises its own service. Sweeping that one
	// races its init. This service only prunes.
	svc := &Service{DB: e.pool}
	svc.init()
	before := metrics.JobErrors.With(worker.JobPushSweep).Load()
	svc.sweepMaintenance(context.Background())
	if n := countSubscriptions(e, bob.id); n != 0 {
		t.Fatalf("%d devices left after the session ended", n)
	}
	if got := metrics.JobErrors.With(worker.JobPushSweep).Load(); got != before {
		t.Fatalf("job errors %d, want %d", got, before)
	}
}

func TestTheSweepCountsAPruneThatCannotReachTheDatabase(t *testing.T) {
	e := newEnv(t, http.StatusCreated)
	bob := e.member("bob")
	cfg := e.pool.Config().Copy()
	cfg.MinConns = 0
	broken, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	broken.Close()
	svc := &Service{DB: &db.Pool{Pool: broken}}
	svc.init()
	before := metrics.JobErrors.With(worker.JobPushSweep).Load()
	svc.sweepMaintenance(context.Background())
	if got := metrics.JobErrors.With(worker.JobPushSweep).Load(); got != before+1 {
		t.Fatalf("job errors %d, want %d", got, before+1)
	}
	if n := countSubscriptions(e, bob.id); n != 1 {
		t.Fatalf("%d devices, want the live one kept", n)
	}
}
