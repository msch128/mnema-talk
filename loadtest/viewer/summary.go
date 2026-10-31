package main

import (
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/msch128/mnema-talk/loadtest/viewer/internal/lt"
)

type memberResult struct {
	ID            string               `json:"id"`
	Role          string               `json:"role"`
	Relayed       bool                 `json:"relayed"`
	SentBytes     int64                `json:"sent_bytes"`
	SentPackets   int64                `json:"sent_packets"`
	Received      map[mediaClass]int64 `json:"received_bytes,omitempty"`
	Connected     bool                 `json:"connected"`
	EverConnected bool                 `json:"ever_connected"`
	ConnectMs     int64                `json:"connect_ms"`
	FirstVideoMs  int64                `json:"first_video_ms"`
	VideoBytes    int64                `json:"video_bytes"`
	AudioTracks   int64                `json:"audio_tracks"`
	Offers        int64                `json:"offers"`
	Flaps         int64                `json:"flaps"`
	CandSent      int64                `json:"candidates_sent"`
	CandRecv      int64                `json:"candidates_received"`
	CandErrors    int64                `json:"candidate_errors"`
	MaxStallMs    int64                `json:"max_stall_ms"`
	Error         string               `json:"error,omitempty"`
	TimedOut      bool                 `json:"ice_timed_out,omitempty"`
	SelectedPair  string               `json:"selected_pair,omitempty"`
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

	// What the headless publishers and the received media add; absent from
	// the older results.
	Relay   bool                       `json:"relay"`
	Relayed int                        `json:"relayed_members"`
	Roles   map[string]int             `json:"roles"`
	Media   map[mediaClass]classResult `json:"media,omitempty"`
	Server  lt.ServerSummary           `json:"server"`
	Stages  *stageSplit                `json:"packet_stages,omitempty"`
	// GeneratorCores is the CPU this tool used itself, in cores on average.
	GeneratorCores float64          `json:"generator_cpu_cores"`
	Thresholds     *voiceThresholds `json:"thresholds,omitempty"`
	Violations     []string         `json:"violations,omitempty"`
	OK             bool             `json:"ok"`
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

// firstVideoMs is the join -> first video byte time, if any video arrived.
func firstVideoMs(m *member) (int64, bool) {
	v := m.videoMs.Load()
	return max(v-1, 0), v > 0
}

// newMemberResult reads one member's counters and diagnostics. The ICE
// details are kept only for members that never connected.
func newMemberResult(m *member) memberResult {
	mr := memberResult{
		ID: m.id, Role: m.role.String(), SentBytes: m.sentB.Load(), SentPackets: m.sentPkt.Load(), Connected: m.connected.Load(), EverConnected: m.everConn.Load(),
		ConnectMs: m.connMs.Load(), VideoBytes: m.videoB.Load(), AudioTracks: m.audioT.Load(),
		Offers: m.offers.Load(), Flaps: m.flaps.Load(), CandSent: m.candSent.Load(),
		CandRecv: m.candRecv.Load(), CandErrors: m.candErr.Load(), MaxStallMs: m.maxStallMs.Load(),
		TimedOut: m.timedOut.Load(),
	}
	if ms, ok := firstVideoMs(m); ok {
		mr.FirstVideoMs = ms
	}
	if f, ok := m.failed.Load().(string); ok {
		mr.Error = f
	}
	m.diagMu.Lock()
	defer m.diagMu.Unlock()
	if m.snapshot != nil {
		mr.SelectedPair = m.snapshot.SelectedPair
		mr.Relayed = strings.HasPrefix(mr.SelectedPair, "relay ")
	}
	if !mr.EverConnected {
		mr.Timeline = append([]string(nil), m.timeline...)
		mr.ServerCandidates = append([]string(nil), m.remoteSeen...)
		mr.ICE = m.snapshot
	}
	return mr
}

// tally adds one member to the result's counters.
func (res *result) tally(m *member, mr memberResult) {
	res.Roles[mr.Role]++
	if mr.Relayed {
		res.Relayed++
	}
	if mr.Error != "" {
		res.Failed++
	}
	if !mr.EverConnected {
		res.NeverConnected++
	}
	if mr.TimedOut {
		res.ICETimedOut++
	}
	if mr.VideoBytes == 0 {
		res.NeverReceived++
	}
	res.Flaps += mr.Flaps
	res.CandidateErrors += mr.CandErrors
	res.MaxStallMs = max(res.MaxStallMs, mr.MaxStallMs)
	res.PerMember = append(res.PerMember, mr)
}

// summarize turns the members' counters and diagnostics into the result.
func summarize(members []*member, loss float64, jitter time.Duration) result {
	res := result{Members: len(members), Loss: loss, JitterMs: jitter.Milliseconds(), Roles: map[string]int{}}
	var conns, firsts []int64
	var media []memberMedia
	var kbps float64
	var kbpsN int
	now := time.Now()
	for _, m := range members {
		mr := newMemberResult(m)
		mm := m.media(now)
		mr.Received = receivedBytes(mm)
		media = append(media, mm)
		if _, ok := firstVideoMs(m); ok {
			firsts = append(firsts, mr.FirstVideoMs)
		}
		if mr.EverConnected {
			conns = append(conns, mr.ConnectMs)
		}
		if mr.VideoBytes > 0 {
			kbps += float64(mr.VideoBytes) * 8 / time.Since(m.startedAt).Seconds() / 1000
			kbpsN++
		}
		res.tally(m, mr)
	}
	res.Media = aggregateMedia(media)
	res.ConnectMsP50, res.ConnectMsP95 = pct(conns, 0.5), pct(conns, 0.95)
	res.FirstVideoMsP50, res.FirstVideoMsP95 = pct(firsts, 0.5), pct(firsts, 0.95)
	if kbpsN > 0 {
		res.AvgVideoKbps = kbps / float64(kbpsN)
	}
	return res
}

// receivedBytes totals a member's received bytes by class.
func receivedBytes(mm memberMedia) map[mediaClass]int64 {
	out := map[mediaClass]int64{}
	for class, tracks := range mm {
		for _, t := range tracks {
			out[class] += t.bytes
		}
	}
	return out
}
