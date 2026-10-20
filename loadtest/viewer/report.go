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
