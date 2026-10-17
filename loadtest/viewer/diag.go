package main

import (
	"fmt"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/pion/webrtc/v4"
)

// maxTimeline bounds the per-member state log.
const maxTimeline = 200

// snapshotTimeout is how long the ICE statistics may take.
const snapshotTimeout = 3 * time.Second

// blockedUDPWrites counts goroutines that wait for a UDP socket to accept a
// datagram.
func blockedUDPWrites() int {
	buf := make([]byte, 16<<20)
	buf = buf[:runtime.Stack(buf, true)]
	n := 0
	for _, g := range strings.Split(string(buf), "\n\n") {
		if strings.Contains(g, "(*FD).WriteTo") && strings.Contains(g, "waitWrite") {
			n++
		}
	}
	return n
}

// event records a state change with the time since the join was sent.
func (m *member) event(format string, args ...any) {
	m.diagMu.Lock()
	defer m.diagMu.Unlock()
	if len(m.timeline) >= maxTimeline {
		return
	}
	m.timeline = append(m.timeline, fmt.Sprintf("%dms %s", time.Since(m.startedAt).Milliseconds(), fmt.Sprintf(format, args...)))
}

// noteRemote remembers a candidate received from the server and what became of it.
func (m *member) noteRemote(c webrtc.ICECandidateInit, how string) {
	m.diagMu.Lock()
	m.remoteSeen = append(m.remoteSeen, how+" "+trimUfrag(c.Candidate))
	m.diagMu.Unlock()
}

// trimUfrag drops the per-connection ufrag from a candidate line.
func trimUfrag(c string) string {
	if i := strings.Index(c, " ufrag "); i >= 0 {
		return c[:i]
	}
	return c
}

// iceSnapshot is the ICE state of one member at one moment, from Pion's statistics.
type iceSnapshot struct {
	// Unresponsive: the ICE agent did not answer for snapshotTimeout.
	// BlockedUDPWrites counts the goroutines of the whole process that are
	// parked in a UDP sendto, the usual reason.
	Unresponsive     bool       `json:"unresponsive,omitempty"`
	BlockedUDPWrites int        `json:"blocked_udp_writes,omitempty"`
	IceState         string     `json:"ice_state"`
	Gathering        string     `json:"gathering_state"`
	Connection       string     `json:"connection_state"`
	SelectedPair     string     `json:"selected_pair,omitempty"`
	LocalCandidates  []string   `json:"local_candidates,omitempty"`
	RemoteCandidates []string   `json:"remote_candidates,omitempty"`
	Pairs            []pairStat `json:"pairs,omitempty"`
}

type pairStat struct {
	Local             string `json:"local"`
	Remote            string `json:"remote"`
	State             string `json:"state"`
	Nominated         bool   `json:"nominated"`
	RequestsSent      uint64 `json:"requests_sent"`
	ResponsesReceived uint64 `json:"responses_received"`
	RequestsReceived  uint64 `json:"requests_received"`
	ResponsesSent     uint64 `json:"responses_sent"`
}

// snapshotICE reads the connection's ICE statistics.
func snapshotICE(pc *webrtc.PeerConnection) *iceSnapshot {
	snap := &iceSnapshot{
		IceState:   pc.ICEConnectionState().String(),
		Gathering:  pc.ICEGatheringState().String(),
		Connection: pc.ConnectionState().String(),
	}
	stats := pc.GetStats()
	cands := map[string]string{}
	for _, st := range stats {
		c, ok := st.(webrtc.ICECandidateStats)
		if !ok {
			continue
		}
		cands[c.ID] = fmt.Sprintf("%s %s:%d", c.CandidateType, c.IP, c.Port)
		if c.Type == webrtc.StatsTypeLocalCandidate {
			snap.LocalCandidates = append(snap.LocalCandidates, cands[c.ID])
		} else {
			snap.RemoteCandidates = append(snap.RemoteCandidates, cands[c.ID])
		}
	}
	for _, st := range stats {
		p, ok := st.(webrtc.ICECandidatePairStats)
		if !ok {
			continue
		}
		ps := pairStat{
			Local: cands[p.LocalCandidateID], Remote: cands[p.RemoteCandidateID],
			State: string(p.State), Nominated: p.Nominated,
			RequestsSent: p.RequestsSent, ResponsesReceived: p.ResponsesReceived,
			RequestsReceived: p.RequestsReceived, ResponsesSent: p.ResponsesSent,
		}
		if p.Nominated && p.State == webrtc.StatsICECandidatePairStateSucceeded {
			snap.SelectedPair = ps.Local + " <-> " + ps.Remote
		}
		snap.Pairs = append(snap.Pairs, ps)
	}
	sort.Strings(snap.LocalCandidates)
	sort.Strings(snap.RemoteCandidates)
	sort.Slice(snap.Pairs, func(i, j int) bool {
		return snap.Pairs[i].Local+snap.Pairs[i].Remote < snap.Pairs[j].Local+snap.Pairs[j].Remote
	})
	return snap
}

// diagnose takes the member's final ICE snapshot, once. Call it before the
// connection is closed.
func (m *member) diagnose() {
	m.snapOnce.Do(func() {
		if m.pc == nil {
			return
		}
		got := make(chan *iceSnapshot, 1)
		go func() { got <- snapshotICE(m.pc) }()
		var snap *iceSnapshot
		select {
		case snap = <-got:
		case <-time.After(snapshotTimeout):
			// Pion answers statistics through the ICE agent's task loop; if
			// that loop is stuck (typically in a UDP write the OS does not
			// let through) the agent never recovers and Close blocks too.
			snap = &iceSnapshot{Unresponsive: true, BlockedUDPWrites: blockedUDPWrites()}
		}
		m.diagMu.Lock()
		m.snapshot = snap
		m.diagMu.Unlock()
	})
}

// diagLines renders the diagnostics of a member for the console.
func (m *member) diagLines() []string {
	m.diagMu.Lock()
	defer m.diagMu.Unlock()
	var out []string
	for _, e := range m.timeline {
		out = append(out, "  timeline "+e)
	}
	for _, c := range m.remoteSeen {
		out = append(out, "  remote   "+c)
	}
	if s := m.snapshot; s != nil {
		if s.Unresponsive {
			out = append(out, fmt.Sprintf("  state    ICE AGENT UNRESPONSIVE (goroutines blocked in a UDP write: %d)", s.BlockedUDPWrites))
		}
		out = append(out, fmt.Sprintf("  state    ice=%s gathering=%s connection=%s", s.IceState, s.Gathering, s.Connection))
		for _, p := range s.Pairs {
			out = append(out, fmt.Sprintf("  pair     %s <-> %s %s sent=%d/%d recv=%d/%d", p.Local, p.Remote, p.State,
				p.ResponsesReceived, p.RequestsSent, p.ResponsesSent, p.RequestsReceived))
		}
	}
	return out
}
