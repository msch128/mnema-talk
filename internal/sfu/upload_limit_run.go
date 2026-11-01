package sfu

import (
	"context"
	"time"

	"github.com/msch128/mnema-talk/internal/metrics"
)

func countUnmet() { metrics.SFUUploadUnmet.Inc() }

// uploadTick is how often the egress is measured and the governor looks at it.
const uploadTick = time.Second

// uploadWireOverhead is what each packet takes on the wire beyond the RTP
// header and payload the egress counter holds: IPv4 (20), UDP (8) and the SRTP
// authentication tag (10). Audio packets are small, so without it the
// estimate of a call of voices alone would be about 40% low. Traffic relayed
// by the built-in TURN relay is not counted at all.
const uploadWireOverhead = 38

// StartUploadGovernor keeps the media the SFU sends under about 85% of mbit
// Mbit/s by stepping viewers on automatic down the layers of screen shares and
// cameras (see upload_limit.go) until ctx ends or the SFU is closed. 0 starts
// nothing.
func (s *SFU) StartUploadGovernor(ctx context.Context, mbit int) {
	if mbit <= 0 {
		return
	}
	ctx, cancel := context.WithCancel(ctx)
	s.uploadMu.Lock()
	if s.closed.Load() || s.uploadStop != nil {
		s.uploadMu.Unlock()
		cancel()
		return
	}
	s.uploadStop = cancel
	s.uploadRuns.Add(1)
	s.uploadWG.Add(1)
	s.uploadMu.Unlock()
	go func() {
		defer s.uploadWG.Done()
		runUploadGovernor(ctx, float64(mbit)*1e6, uploadTick, uploadSource{
			egressBytes:   metrics.SFUEgressBytes.Load,
			egressPackets: metrics.SFUEgressPackets.Load,
			views:         s.uploadViews,
			now:           time.Now,
		})
	}()
}

// stopUploadGovernor ends the governor and waits for it.
func (s *SFU) stopUploadGovernor() {
	s.uploadMu.Lock()
	stop := s.uploadStop
	s.uploadMu.Unlock()
	if stop != nil {
		stop()
	}
	s.uploadWG.Wait()
}

// uploadSource is what the governor loop reads; tests supply their own.
type uploadSource struct {
	egressBytes   func() uint64
	egressPackets func() uint64
	views         func(now time.Time) []uploadView
	now           func() time.Time
}

// egressMeter turns the byte and packet counters into a smoothed rate in bits
// per second, wire overhead included.
type egressMeter struct {
	bytes, packets uint64
	at             time.Time
	rate           float64
}

func newEgressMeter(bytes, packets uint64, now time.Time) *egressMeter {
	return &egressMeter{bytes: bytes, packets: packets, at: now}
}

// sample takes the counters at now and returns the smoothed rate: half the
// previous rate and half what was measured since the last sample.
func (m *egressMeter) sample(bytes, packets uint64, now time.Time) float64 {
	if dt := now.Sub(m.at).Seconds(); dt > 0 {
		wire := (bytes - m.bytes) + (packets-m.packets)*uploadWireOverhead
		m.rate = m.rate*0.5 + float64(wire)*8/dt*0.5
	}
	m.bytes, m.packets, m.at = bytes, packets, now
	return m.rate
}

func runUploadGovernor(ctx context.Context, limit float64, tick time.Duration, src uploadSource) {
	g := &uploadGovernor{limit: limit}
	metrics.SFUUploadLimit.Set(int64(limit))
	defer func() {
		metrics.SFUUploadLimit.Set(0)
		metrics.SFUUploadEgress.Set(0)
		metrics.SFUUploadCapped.Set(0)
	}()
	meter := newEgressMeter(src.egressBytes(), src.egressPackets(), src.now())
	ticker := time.NewTicker(tick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		now := src.now()
		egress := meter.sample(src.egressBytes(), src.egressPackets(), now)
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
