package main

import (
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"
)

// mediaClass is what a received track carries. The SFU names the stream of a
// camera "cam:<user>" and of a screen share's sound "screen:<user>"; the voice
// and the screen video use the plain user id.
type mediaClass string

const (
	classScreen      mediaClass = "screen"
	classCamera      mediaClass = "camera"
	classAudio       mediaClass = "audio"
	classScreenAudio mediaClass = "screen_audio"
)

var allClasses = []mediaClass{classScreen, classCamera, classAudio, classScreenAudio}

func (c mediaClass) video() bool { return c == classScreen || c == classCamera }

func classify(kind webrtc.RTPCodecType, streamID string) mediaClass {
	switch {
	case strings.HasPrefix(streamID, "cam:"):
		return classCamera
	case strings.HasPrefix(streamID, "screen:"):
		return classScreenAudio
	case kind == webrtc.RTPCodecTypeVideo:
		return classScreen
	}
	return classAudio
}

// trackStats is what one received track shows: bytes, the packets that went
// missing (a gap in the sequence numbers that was never filled, so what NACK
// repaired does not count), and the stretches without any packet.
type trackStats struct {
	class     mediaClass
	freezeGap time.Duration // a gap above this is a freeze (video only)
	mu        sync.Mutex
	packets   int64
	bytes     int64
	haveSeq   bool
	baseSeq   uint32
	maxSeq    uint32 // extended: counts wraps
	first     time.Time
	last      time.Time
	freezes   int64
	maxGap    time.Duration
}

// freezeGap is the gap in a video stream that counts as a freeze (FREEZE_MS).
func freezeGap() time.Duration {
	return time.Duration(envInt("FREEZE_MS", 1000)) * time.Millisecond
}

func newTrackStats(class mediaClass, freezeGap time.Duration) *trackStats {
	return &trackStats{class: class, freezeGap: freezeGap}
}

// observe records one packet of n bytes with sequence number seq read at now.
func (t *trackStats) observe(now time.Time, n int, seq uint16) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.packets == 0 {
		t.first = now
	} else {
		t.noteGap(now.Sub(t.last))
	}
	t.last = now
	t.packets++
	t.bytes += int64(n)
	switch {
	case !t.haveSeq:
		t.haveSeq, t.baseSeq, t.maxSeq = true, uint32(seq), uint32(seq)
	default:
		// A positive 16-bit distance is progress (also across the wrap); a
		// negative one is a late or repeated packet.
		if d := int16(seq - uint16(t.maxSeq)); d > 0 {
			t.maxSeq += uint32(d)
		}
	}
}

func (t *trackStats) noteGap(gap time.Duration) {
	t.maxGap = max(t.maxGap, gap)
	if t.class.video() && t.freezeGap > 0 && gap > t.freezeGap {
		t.freezes++
	}
}

// trackSnapshot is a track's numbers at one moment.
type trackSnapshot struct {
	class    mediaClass
	packets  int64
	bytes    int64
	expected int64
	lost     int64
	freezes  int64
	maxGap   time.Duration
	span     time.Duration // first to last packet
}

// snapshot reads the numbers at now. A stretch without packets that is still
// going on counts as a freeze when it is long enough.
func (t *trackStats) snapshot(now time.Time) trackSnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := trackSnapshot{class: t.class, packets: t.packets, bytes: t.bytes, freezes: t.freezes, maxGap: t.maxGap}
	if t.packets == 0 {
		return s
	}
	s.span = t.last.Sub(t.first)
	s.expected = int64(t.maxSeq-t.baseSeq) + 1
	s.lost = max(s.expected-t.packets, 0)
	if ongoing := now.Sub(t.last); ongoing > t.maxGap {
		s.maxGap = ongoing
		if t.class.video() && t.freezeGap > 0 && ongoing > t.freezeGap {
			s.freezes++
		}
	}
	return s
}

// classResult is what the members received of one class of media.
type classResult struct {
	Members          int     `json:"members"`
	Tracks           int     `json:"tracks"`
	Packets          int64   `json:"packets"`
	KbpsPerMemberAvg float64 `json:"kbps_per_member_avg"`
	KbpsPerMemberMin float64 `json:"kbps_per_member_min"`
	KbpsPerMemberMax float64 `json:"kbps_per_member_max"`
	KbpsPerTrackAvg  float64 `json:"kbps_per_track_avg"`
	PacketsExpected  int64   `json:"packets_expected"`
	PacketsLost      int64   `json:"packets_lost"`
	LossPct          float64 `json:"loss_pct"`
	Freezes          int64   `json:"freezes"`
	MembersFrozen    int     `json:"members_frozen"`
	MaxGapMs         int64   `json:"max_gap_ms"`
}

// memberMedia is one member's snapshots, by class.
type memberMedia map[mediaClass][]trackSnapshot

// aggregateMedia combines the members' tracks into one result per class that
// anybody received.
func aggregateMedia(members []memberMedia) map[mediaClass]classResult {
	out := map[mediaClass]classResult{}
	for _, class := range allClasses {
		if r, ok := aggregateClass(class, members); ok {
			out[class] = r
		}
	}
	return out
}

func aggregateClass(class mediaClass, members []memberMedia) (classResult, bool) {
	var r classResult
	var perMember, perTrack []float64
	for _, mm := range members {
		tracks := mm[class]
		if len(tracks) == 0 {
			continue
		}
		r.Members++
		kbps, frozen := foldTracks(&r, tracks, &perTrack)
		perMember = append(perMember, kbps)
		if frozen {
			r.MembersFrozen++
		}
	}
	if r.Members == 0 {
		return r, false
	}
	if r.PacketsExpected > 0 {
		r.LossPct = float64(r.PacketsLost) / float64(r.PacketsExpected) * 100
	}
	r.KbpsPerMemberAvg, r.KbpsPerMemberMin, r.KbpsPerMemberMax = spread(perMember)
	r.KbpsPerTrackAvg, _, _ = spread(perTrack)
	return r, true
}

// foldTracks adds one member's tracks of a class to r and returns the member's
// total rate and whether any of its tracks froze.
func foldTracks(r *classResult, tracks []trackSnapshot, perTrack *[]float64) (float64, bool) {
	var kbps float64
	frozen := false
	for _, s := range tracks {
		r.Tracks++
		r.Packets += s.packets
		r.PacketsExpected += s.expected
		r.PacketsLost += s.lost
		r.Freezes += s.freezes
		r.MaxGapMs = max(r.MaxGapMs, s.maxGap.Milliseconds())
		frozen = frozen || s.freezes > 0
		if s.span > 0 {
			k := float64(s.bytes) * 8 / s.span.Seconds() / 1000
			kbps += k
			*perTrack = append(*perTrack, k)
		}
	}
	return kbps, frozen
}

// spread returns the mean, minimum and maximum.
func spread(v []float64) (avg, lo, hi float64) {
	if len(v) == 0 {
		return 0, 0, 0
	}
	sort.Float64s(v)
	var sum float64
	for _, x := range v {
		sum += x
	}
	return sum / float64(len(v)), v[0], v[len(v)-1]
}

// addTrack registers a received track with the member.
func (m *member) addTrack(ts *trackStats) {
	m.trackMu.Lock()
	m.tracks = append(m.tracks, ts)
	m.trackMu.Unlock()
}

// media returns the member's received media by class at now.
func (m *member) media(now time.Time) memberMedia {
	m.trackMu.Lock()
	tracks := append([]*trackStats(nil), m.tracks...)
	m.trackMu.Unlock()
	out := memberMedia{}
	for _, t := range tracks {
		s := t.snapshot(now)
		out[s.class] = append(out[s.class], s)
	}
	return out
}
