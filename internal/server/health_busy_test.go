package server

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// After a restart every client reads its state again at once and the ping
// waits behind them for a connection. While the others keep getting theirs,
// the database is busy, not down: GET /api/health must not answer 503 then.
func TestPingOrBusy(t *testing.T) {
	timedOut := fmt.Errorf("acquire: %w", context.DeadlineExceeded)
	refused := errors.New("connection refused")
	cases := []struct {
		name     string
		err      error
		progress int64 // connections handed out while the ping waited
		down     bool
	}{
		{"answered", nil, 0, false},
		{"timed out while others got connections", timedOut, 40, false},
		{"timed out and nothing moved", timedOut, 0, true},
		{"timed out after taking only its own connection", timedOut, 1, true},
		{"refused", refused, 40, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var acquired int64
			err := pingOrBusy(context.Background(), func(context.Context) error {
				acquired += c.progress
				return c.err
			}, func() int64 { return acquired })
			if (err != nil) != c.down {
				t.Fatalf("err %v, want down=%v", err, c.down)
			}
		})
	}
}
