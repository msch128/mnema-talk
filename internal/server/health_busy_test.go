package server

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/mnema-xyz/talk/internal/db"
)

// After a restart every client reads its state again at once and the ping
// waits behind them for a connection. While the others keep finishing their
// work, the database is busy, not down: GET /api/health must not answer 503.
// A database that hangs still hands out its idle connections, but none comes
// back: that is down.
func TestPingOrBusy(t *testing.T) {
	timedOut := fmt.Errorf("acquire: %w", context.DeadlineExceeded)
	refused := errors.New("connection refused")
	cases := []struct {
		name             string
		err              error
		handed, returned int64 // connections handed out, and given back, while the ping waited
		down             bool
	}{
		{"answered", nil, 0, 0, false},
		{"timed out while others finished their work", timedOut, 40, 40, false},
		{"timed out and nothing moved", timedOut, 0, 0, true},
		{"timed out after only its own connection came back", timedOut, 1, 1, true},
		{"hung: idle connections handed out, none came back", timedOut, 20, 0, true},
		{"refused", refused, 40, 40, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := db.PoolStats{AcquireCount: 100, Acquired: 5}
			err := pingOrBusy(context.Background(), func(context.Context) error {
				s.AcquireCount += c.handed
				s.Acquired += int32(c.handed - c.returned)
				return c.err
			}, func() db.PoolStats { return s })
			if (err != nil) != c.down {
				t.Fatalf("err %v, want down=%v", err, c.down)
			}
		})
	}
}
