package main

import (
	"fmt"
	"os"
	"time"
)

// sampler tracks how long each member goes without new video bytes.
type sampler struct {
	members  []*member
	last     map[*member]int64
	lastGrow map[*member]time.Time
}

// sample records each member's video growth at now: stall = longest stretch
// without new video bytes after the first ones.
func (s *sampler) sample(now time.Time) {
	for _, m := range s.members {
		v := m.videoB.Load()
		g, started := s.lastGrow[m]
		if v > s.last[m] {
			s.last[m] = v
			s.lastGrow[m] = now
		}
		if started {
			if d := now.Sub(g).Milliseconds(); d > m.maxStallMs.Load() {
				m.maxStallMs.Store(d)
			}
		}
	}
}

// printProgress prints one status line for the run so far.
func (s *sampler) printProgress(now time.Time) {
	var connected, failed, receiving int
	var offers int64
	for _, m := range s.members {
		if m.connected.Load() {
			connected++
		}
		if g, ok := s.lastGrow[m]; ok && now.Sub(g) < 3*time.Second {
			receiving++
		}
		offers += m.offers.Load()
		if m.failed.Load() != nil {
			failed++
		}
	}
	fmt.Printf("%s viewers=%d connected=%d receivingScreen=%d failed=%d offers=%d\n",
		now.Format("15:04:05"), len(s.members), connected, receiving, failed, offers)
}

// watchMembers samples every 250 ms and prints progress every 10 s until hold
// has passed or stopFile exists.
func watchMembers(members []*member, hold time.Duration, stopFile string) {
	s := &sampler{members: members, last: map[*member]int64{}, lastGrow: map[*member]time.Time{}}
	start := time.Now()
	next := start.Add(10 * time.Second)
	for time.Since(start) < hold {
		time.Sleep(250 * time.Millisecond)
		now := time.Now()
		if stopFile != "" {
			if _, err := os.Stat(stopFile); err == nil {
				break
			}
		}
		s.sample(now)
		if now.Before(next) {
			continue
		}
		next = now.Add(10 * time.Second)
		s.printProgress(now)
	}
}

// printReport prints the members that never connected, with their
// diagnostics, and the run's summary line.
func printReport(members []*member, res result) {
	for _, mr := range res.PerMember {
		if mr.EverConnected {
			continue
		}
		fmt.Printf("member %s never connected (err=%s)\n", mr.ID[:min(8, len(mr.ID))], mr.Error)
		printMemberDiags(members, mr.ID)
	}
	fmt.Printf("viewers=%d neverConnected=%d iceTimedOut=%d neverReceived=%d failed=%d flaps=%d connect p50/p95=%d/%dms firstVideo p50/p95=%d/%dms maxStall=%dms avgKbps=%.0f\n",
		res.Members, res.NeverConnected, res.ICETimedOut, res.NeverReceived, res.Failed, res.Flaps,
		res.ConnectMsP50, res.ConnectMsP95, res.FirstVideoMsP50, res.FirstVideoMsP95, res.MaxStallMs, res.AvgVideoKbps)
	printMedia(res)
}

// printMedia prints what the members received, per class, and the verdict.
func printMedia(res result) {
	for _, class := range allClasses {
		r, ok := res.Media[class]
		if !ok {
			continue
		}
		fmt.Printf("%-12s %d members, %d tracks, per member %.0f kbit/s (min %.0f, max %.0f), per track %.0f kbit/s, loss %.2f%% (%d of %d packets), freezes %d in %d members, longest gap %d ms\n",
			class, r.Members, r.Tracks, r.KbpsPerMemberAvg, r.KbpsPerMemberMin, r.KbpsPerMemberMax, r.KbpsPerTrackAvg,
			r.LossPct, r.PacketsLost, r.PacketsExpected, r.Freezes, r.MembersFrozen, r.MaxGapMs)
	}
	if res.Stages != nil {
		for _, l := range res.Stages.lines() {
			fmt.Println(l)
		}
	}
	if res.Relay {
		fmt.Printf("relay        %d of %d members went through the TURN relay\n", res.Relayed, res.Members-res.NeverConnected)
	}
	if s := res.Server; s.Measured {
		fmt.Printf("server       CPU %.2f cores in hold (%.2f over the whole load, peak %.2f), memory peak %.0f MB resident / %.0f MB heap, goroutines %.0f -> peak %.0f -> %.0f\n",
			s.CPUCoresHold, s.CPUCoresAvg, s.CPUCoresPeak, s.RSSPeakMB, s.HeapPeakMB, s.GoroutinesPre, s.GoroutinesPeak, s.GoroutinesPost)
	}
	fmt.Printf("generator    this tool used %.2f cores on average (a generator near the host's core count measures itself)\n", res.GeneratorCores)
	if res.Thresholds == nil {
		return
	}
	if res.OK {
		fmt.Println("OK: all thresholds met")
		return
	}
	fmt.Printf("FAILED (%d):\n", len(res.Violations))
	for _, v := range res.Violations {
		fmt.Printf("  - %s\n", v)
	}
}

func printMemberDiags(members []*member, id string) {
	for _, m := range members {
		if m.id != id {
			continue
		}
		for _, l := range m.diagLines() {
			fmt.Println(l)
		}
	}
}
