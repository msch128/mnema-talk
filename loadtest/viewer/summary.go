package main

import (
	"sort"
	"sync"
	"time"
)

type memberResult struct {
	ID            string `json:"id"`
	Connected     bool   `json:"connected"`
	EverConnected bool   `json:"ever_connected"`
	ConnectMs     int64  `json:"connect_ms"`
	FirstVideoMs  int64  `json:"first_video_ms"`
	VideoBytes    int64  `json:"video_bytes"`
	AudioTracks   int64  `json:"audio_tracks"`
	Offers        int64  `json:"offers"`
	Flaps         int64  `json:"flaps"`
	CandSent      int64  `json:"candidates_sent"`
	CandRecv      int64  `json:"candidates_received"`
	CandErrors    int64  `json:"candidate_errors"`
	MaxStallMs    int64  `json:"max_stall_ms"`
	Error         string `json:"error,omitempty"`
	TimedOut      bool   `json:"ice_timed_out,omitempty"`
	SelectedPair  string `json:"selected_pair,omitempty"`
	// Timeline, ServerCandidates and ICE are set for members that never
	// connected: what happened and what Pion knew.
	Timeline         []string     `json:"timeline,omitempty"`
	ServerCandidates []string     `json:"server_candidates,omitempty"`
	ICE              *iceSnapshot `json:"ice,omitempty"`
}

type result struct {
	Members         int            `json:"members"`
	NeverConnected  int            `json:"never_connected"`
	ICETimedOut     int            `json:"ice_timed_out"`
	NeverReceived   int            `json:"never_received"`
	Failed          int            `json:"failed"`
	Flaps           int64          `json:"flaps"`
	ConnectMsP50    int64          `json:"connect_ms_p50"`
	ConnectMsP95    int64          `json:"connect_ms_p95"`
	FirstVideoMsP50 int64          `json:"first_video_ms_p50"`
	FirstVideoMsP95 int64          `json:"first_video_ms_p95"`
	MaxStallMs      int64          `json:"max_stall_ms"`
	AvgVideoKbps    float64        `json:"avg_video_kbps"`
	CandidateErrors int64          `json:"candidate_errors"`
	Loss            float64        `json:"loss"`
	JitterMs        int64          `json:"jitter_ms"`
	PerMember       []memberResult `json:"per_member"`
}

func pct(v []int64, q float64) int64 {
	if len(v) == 0 {
		return 0
	}
	sort.Slice(v, func(i, j int) bool { return v[i] < v[j] })
	return v[min(len(v)-1, int(q*float64(len(v))))]
}

// diagnoseAll snapshots all members concurrently and waits at most limit.
func diagnoseAll(members []*member, limit time.Duration) {
	var wg sync.WaitGroup
	for _, m := range members {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.diagnose()
		}()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(limit):
	}
}

// summarize turns the members' counters and diagnostics into the result.
func summarize(members []*member, loss float64, jitter time.Duration) result {
	res := result{Members: len(members), Loss: loss, JitterMs: jitter.Milliseconds()}
	var conns, firsts []int64
	var kbps float64
	var kbpsN int
	for _, m := range members {
		mr := memberResult{
			ID: m.id, Connected: m.connected.Load(), EverConnected: m.everConn.Load(),
			ConnectMs: m.connMs.Load(), VideoBytes: m.videoB.Load(), AudioTracks: m.audioT.Load(),
			Offers: m.offers.Load(), Flaps: m.flaps.Load(), CandSent: m.candSent.Load(),
			CandRecv: m.candRecv.Load(), CandErrors: m.candErr.Load(), MaxStallMs: m.maxStallMs.Load(),
			TimedOut: m.timedOut.Load(),
		}
		if v := m.videoMs.Load(); v > 0 {
			mr.FirstVideoMs = v - 1
			firsts = append(firsts, mr.FirstVideoMs)
		}
		if f, ok := m.failed.Load().(string); ok {
			mr.Error = f
			res.Failed++
		}
		m.diagMu.Lock()
		if m.snapshot != nil {
			mr.SelectedPair = m.snapshot.SelectedPair
		}
		if !mr.EverConnected {
			mr.Timeline = append([]string(nil), m.timeline...)
			mr.ServerCandidates = append([]string(nil), m.remoteSeen...)
			mr.ICE = m.snapshot
		}
		m.diagMu.Unlock()
		if !mr.EverConnected {
			res.NeverConnected++
		} else {
			conns = append(conns, mr.ConnectMs)
		}
		if mr.TimedOut {
			res.ICETimedOut++
		}
		if mr.VideoBytes == 0 {
			res.NeverReceived++
		} else {
			kbps += float64(mr.VideoBytes) * 8 / time.Since(m.startedAt).Seconds() / 1000
			kbpsN++
		}
		res.Flaps += mr.Flaps
		res.CandidateErrors += mr.CandErrors
		res.MaxStallMs = max(res.MaxStallMs, mr.MaxStallMs)
		res.PerMember = append(res.PerMember, mr)
	}
	res.ConnectMsP50, res.ConnectMsP95 = pct(conns, 0.5), pct(conns, 0.95)
	res.FirstVideoMsP50, res.FirstVideoMsP95 = pct(firsts, 0.5), pct(firsts, 0.95)
	if kbpsN > 0 {
		res.AvgVideoKbps = kbps / float64(kbpsN)
	}
	return res
}
