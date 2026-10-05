package ws

import (
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
)

// Events reach a client from goroutines other than its read loop (SFU
// signalling callbacks, admin kicks). Unregistering must not make those
// sends panic with "send on closed channel".
func TestSendAfterUnregisterDoesNotPanic(t *testing.T) {
	h := NewHub(nil, nil, nil, nil)
	for i := 0; i < 50; i++ {
		c := newClient(h, nil, auth.User{ID: uuid.New(), Username: "u"}, 0)
		h.register(c)
		var wg sync.WaitGroup
		for j := 0; j < 4; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for k := 0; k < 100; k++ {
					c.SendEvent("webrtc_candidate", map[string]any{"k": k})
				}
			}()
		}
		h.unregister(c)
		wg.Wait()
		c.SendEvent("voice_kicked", nil) // after unregister: dropped, no panic
		select {
		case <-c.done:
		default:
			t.Fatal("done not closed after unregister")
		}
	}
}
