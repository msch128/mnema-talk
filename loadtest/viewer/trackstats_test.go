package main

import (
	"testing"
	"time"
)

var t0 = time.Unix(2000, 0)

func feed(ts *trackStats, start time.Time, seqs []uint16, every time.Duration) time.Time {
	now := start
	for _, s := range seqs {
		ts.observe(now, 1000, s)
		now = now.Add(every)
	}
	return now
}

func TestUnfilledGapsCountAsLossAndLatePacketsFillThem(t *testing.T) {
	ts := newTrackStats(classScreen, time.Second)
	// 10..19 with 13 and 14 never arriving, and 16 arriving late after 17.
	feed(ts, t0, []uint16{10, 11, 12, 15, 17, 16, 18, 19}, 20*time.Millisecond)
	s := ts.snapshot(t0.Add(time.Second))
	if s.expected != 10 || s.packets != 8 || s.lost != 2 {
		t.Fatalf("snapshot = %+v", s)
	}
}

func TestSequenceWrapIsNotLoss(t *testing.T) {
	ts := newTrackStats(classCamera, time.Second)
	feed(ts, t0, []uint16{65533, 65534, 65535, 0, 1, 2}, 20*time.Millisecond)
	s := ts.snapshot(t0.Add(time.Second))
	if s.expected != 6 || s.lost != 0 {
		t.Fatalf("snapshot = %+v", s)
	}
}

func TestOnlyVideoGapsAboveTheThresholdAreFreezes(t *testing.T) {
	video := newTrackStats(classCamera, 500*time.Millisecond)
	audio := newTrackStats(classAudio, 500*time.Millisecond)
	for _, ts := range []*trackStats{video, audio} {
		ts.observe(t0, 100, 1)
		ts.observe(t0.Add(400*time.Millisecond), 100, 2)  // under the threshold
		ts.observe(t0.Add(1400*time.Millisecond), 100, 3) // one second without packets
		ts.observe(t0.Add(1420*time.Millisecond), 100, 4)
	}
	if s := video.snapshot(t0.Add(1430 * time.Millisecond)); s.freezes != 1 || s.maxGap != time.Second {
		t.Fatalf("video = %+v", s)
	}
	if s := audio.snapshot(t0.Add(1430 * time.Millisecond)); s.freezes != 0 || s.maxGap != time.Second {
		t.Fatalf("audio = %+v", s)
	}
}

func TestAFreezeStillGoingOnAtTheEndCounts(t *testing.T) {
	ts := newTrackStats(classScreen, time.Second)
	ts.observe(t0, 100, 1)
	ts.observe(t0.Add(30*time.Millisecond), 100, 2)
	if s := ts.snapshot(t0.Add(500 * time.Millisecond)); s.freezes != 0 {
		t.Fatalf("a short silence counted: %+v", s)
	}
	s := ts.snapshot(t0.Add(5 * time.Second))
	if s.freezes != 1 || s.maxGap < 4*time.Second {
		t.Fatalf("snapshot = %+v", s)
	}
	// The snapshot must not change the track: a later packet ends the freeze
	// once, not twice.
	ts.observe(t0.Add(5*time.Second), 100, 3)
	if s := ts.snapshot(t0.Add(5 * time.Second)); s.freezes != 1 {
		t.Fatalf("freezes = %d after the stream resumed", s.freezes)
	}
}

func TestAggregationGivesRatesLossAndFrozenMembers(t *testing.T) {
	mk := func(bytes int64, span time.Duration, expected, lost, freezes int64) trackSnapshot {
		return trackSnapshot{class: classCamera, bytes: bytes, span: span, expected: expected, lost: lost, freezes: freezes}
	}
	members := []memberMedia{
		{classCamera: {mk(125_000, time.Second, 100, 0, 0), mk(125_000, time.Second, 100, 0, 0)}}, // 2 Mbit/s in two tracks
		{classCamera: {mk(125_000, time.Second, 100, 10, 2)}},                                     // 1 Mbit/s, lossy, frozen
		{classAudio: {{class: classAudio, bytes: 4000, span: time.Second, expected: 50}}},
	}
	got := aggregateMedia(members)
	cam := got[classCamera]
	if cam.Members != 2 || cam.Tracks != 3 || cam.KbpsPerMemberAvg != 1500 || cam.KbpsPerMemberMin != 1000 || cam.KbpsPerMemberMax != 2000 {
		t.Fatalf("camera = %+v", cam)
	}
	if cam.KbpsPerTrackAvg != 1000 || cam.PacketsLost != 10 || cam.Freezes != 2 || cam.MembersFrozen != 1 {
		t.Fatalf("camera = %+v", cam)
	}
	if cam.LossPct < 3.3 || cam.LossPct > 3.4 {
		t.Fatalf("loss = %v", cam.LossPct)
	}
	if _, ok := got[classScreen]; ok {
		t.Fatal("a class nobody received is listed")
	}
	if a := got[classAudio]; a.Members != 1 || a.KbpsPerMemberAvg != 32 {
		t.Fatalf("audio = %+v", a)
	}
}
