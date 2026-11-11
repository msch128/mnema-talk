package sfu

import (
	"testing"
	"time"

	"github.com/pion/rtp"
)

// Every layer is forwarded by a goroutine of its own, and each reads the
// clock before it takes the viewer's lock. The keyframe that starts a new
// layer can therefore carry a reading that is older than the one of the last
// packet sent, or one that lies hours after it.
func TestLayerSwitchAlwaysMovesTheTimestampForward(t *testing.T) {
	for _, tc := range []struct {
		name    string
		elapsed time.Duration
		want    uint32
	}{
		{"clock reading one tick older than the last packet", -15 * time.Microsecond, 1},
		{"clock reading much older than the last packet", -3 * time.Second, 1},
		{"same instant", 0, 1},
		{"one frame later", 33 * time.Millisecond, 2970},
		{"longer than the timestamp can express", 48 * time.Hour, maxSwitchTicks},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var sent []rtp.Header
			vt := &viewerTrack{group: &layerGroup{kind: codecVP8, source: SourceScreen}, cur: -1, eff: 0}
			vt.sink = func(p *rtp.Packet) error {
				sent = append(sent, p.Header)
				return nil
			}
			at := time.Unix(1_700_000_000, 0)
			offer := func(layer int, seq uint16, ts uint32, now time.Time) {
				vt.mu.Lock()
				defer vt.mu.Unlock()
				vt.writeLocked(layer, &rtp.Packet{
					Header:  rtp.Header{Version: 2, SequenceNumber: seq, Timestamp: ts},
					Payload: vp8Payload(true, true, 7, true, 1, byte(layer)),
				}, now)
			}

			offer(0, 100, 1_450_000, at)
			vt.eff = 1
			offer(1, 20_000, 2_000_000, at.Add(tc.elapsed))

			if len(sent) != 2 {
				t.Fatalf("the viewer received %d packets, want the last of layer 0 and the keyframe of layer 1", len(sent))
			}
			if got := sent[1].Timestamp - sent[0].Timestamp; got != tc.want {
				t.Fatalf("the timestamp moved %d ticks across the switch (%d to %d), want %d",
					int32(got), sent[0].Timestamp, sent[1].Timestamp, tc.want)
			}
		})
	}
}
