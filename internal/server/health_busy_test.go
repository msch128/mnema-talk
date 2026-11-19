package server

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// After a restart every client reads its state again at once and the ping
// waits behind them for a connection. While the database keeps finishing other
// queries it is busy, not down: GET /api/health must not answer 503. A
// database that hangs finishes nothing, however many connections the pool
// hands out or destroys meanwhile: that is down.
func TestPingOrBusy(t *testing.T) {
	timedOut := fmt.Errorf("acquire: %w", context.DeadlineExceeded)
	refused := errors.New("connection refused")
	cases := []struct {
		name string
		err  error
		done int64 // queries that completed without an error while the ping waited
		down bool
	}{
		{"answered", nil, 0, false},
		{"timed out while other queries completed", timedOut, 40, false},
		{"timed out and nothing completed", timedOut, 0, true},
		{"timed out after a single query completed", timedOut, 1, true},
		// Idle connections handed out to requests, then destroyed when those
		// requests are cancelled: the pool counters move, no query finishes.
		{"hung: connections destroyed by cancelled requests, no successful query", timedOut, 0, true},
		{"refused", refused, 40, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var done int64
			err := pingOrBusy(context.Background(), func(context.Context) error {
				done += c.done
				return c.err
			}, func() int64 { return done })
			if (err != nil) != c.down {
				t.Fatalf("err %v, want down=%v", err, c.down)
			}
		})
	}
}
