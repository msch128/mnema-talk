package sfu

import (
	"context"
	"time"

	"github.com/msch128/mnema-talk/internal/metrics"
)

func countUnmet() { metrics.SFUUploadUnmet.Inc() }

// uploadTick is how often the egress is measured and the governor looks at it.
const uploadTick = time.Second

// StartUploadGovernor keeps the media the SFU sends under about 85% of mbit
// Mbit/s by stepping viewers on automatic down the layers of screen shares and
// cameras (see upload_limit.go) until ctx ends. 0 starts nothing.
func (s *SFU) StartUploadGovernor(ctx context.Context, mbit int) {
	if mbit <= 0 {
		return
	}
	go runUploadGovernor(ctx, float64(mbit)*1e6, uploadTick, uploadSource{
		egressBytes: metrics.SFUEgressBytes.Load,
		views:       s.uploadViews,
		now:         time.Now,
	})
}

// uploadSource is what the governor loop reads; tests supply their own.
type uploadSource struct {
	egressBytes func() uint64
	views       func(now time.Time) []uploadView
	now         func() time.Time
}

func runUploadGovernor(ctx context.Context, limit float64, tick time.Duration, src uploadSource) {
	g := &uploadGovernor{limit: limit}
	metrics.SFUUploadLimit.Set(int64(limit))
	defer metrics.SFUUploadLimit.Set(0)
	last, lastAt := src.egressBytes(), src.now()
	var egress float64
	ticker := time.NewTicker(tick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		now := src.now()
		bytes := src.egressBytes()
		if dt := now.Sub(lastAt).Seconds(); dt > 0 {
			inst := float64(bytes-last) * 8 / dt
			egress = egress*0.5 + inst*0.5
		}
		last, lastAt = bytes, now
		metrics.SFUUploadEgress.Set(int64(egress))
		metrics.SFUUploadCapped.Set(int64(g.step(now, egress, src.views(now))))
	}
}

// uploadViews describes every viewer on automatic of every layered screen
// share and camera.
func (s *SFU) uploadViews(now time.Time) []uploadView {
	s.roomsMu.Lock()
	rooms := make([]*Room, 0, len(s.rooms))
	for _, r := range s.rooms {
		rooms = append(rooms, r)
	}
	s.roomsMu.Unlock()
	var out []uploadView
	for _, r := range rooms {
		for _, g := range r.layerGroups() {
			list := g.snapshot.Load()
			if list == nil {
				continue
			}
			for _, vt := range *list {
				if v, ok := vt.uploadView(now); ok {
					out = append(out, v)
				}
			}
		}
	}
	return out
}

// layerGroups lists the room's screen and camera layer groups.
func (r *Room) layerGroups() []*layerGroup {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*layerGroup, 0, len(r.groups)+len(r.cameraGroups))
	for _, g := range r.groups {
		out = append(out, g)
	}
	for _, g := range r.cameraGroups {
		out = append(out, g)
	}
	return out
}
