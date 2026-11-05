package chat

import (
	"testing"

	"github.com/msch128/mnema-talk/internal/chat/channels"
)

// FuzzSlowModeLimit checks the slow-mode value is accepted exactly from off
// (0) to the 24 hour limit, for every integer a client can send.
func FuzzSlowModeLimit(f *testing.F) {
	for _, n := range []int{0, 1, 5, 3600, MaxSlowModeSeconds, MaxSlowModeSeconds + 1, -1, 1 << 62, -(1 << 62)} {
		f.Add(n)
	}
	f.Fuzz(func(t *testing.T, n int) {
		err := channels.ValidateChannelLimits(nil, &n)
		if ok := n >= 0 && n <= MaxSlowModeSeconds; ok != (err == nil) {
			t.Fatalf("slow mode %d: error %v", n, err)
		}
	})
}
