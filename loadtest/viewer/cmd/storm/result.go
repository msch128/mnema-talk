package main

import (
	"time"

	"github.com/mnema-xyz/talk/loadtest/viewer/internal/lt"
)

type result struct {
	Sessions  int    `json:"sessions"`
	Backoff   string `json:"backoff"`
	AllBack   bool   `json:"all_back"`
	BackCount int64  `json:"back"`
	Drops     int64  `json:"drops"`
	// AllBackS is the time from the first drop until the last session was
	// connected and synced again.
	AllBackS  float64                  `json:"all_back_s"`
	Back      lt.Summary               `json:"back_after_drop"`
	Upgrade   lt.Summary               `json:"upgrade"`
	FirstSync lt.Summary               `json:"first_sync"`
	Statuses  map[string]map[int]int64 `json:"statuses"`
	Latency   map[string]lt.Summary    `json:"latency"`
	Requests  int64                    `json:"requests"`
	Refused   int64                    `json:"refused_429"`
	Failed    int64                    `json:"failed"`
	// Unreachable requests found no server (it was restarting).
	Unreachable int64 `json:"unreachable"`
}

func (r *run) result(allBack bool) *result {
	res := &result{
		Sessions: r.cfg.sessions, Backoff: r.cfg.backoff,
		AllBack: allBack, BackCount: r.backN.Load(), Drops: r.drops.Load(),
		Back: r.back.Load().Summarize(), Upgrade: r.upgrade.Load().Summarize(), FirstSync: r.firstSync.Load().Summarize(),
	}
	if allBack {
		res.AllBackS = time.Duration(r.back.Load().Summarize().MaxMs * float64(time.Millisecond)).Seconds()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	res.Statuses = r.statuses
	res.Latency = map[string]lt.Summary{}
	for name, h := range r.latency {
		res.Latency[name] = h.Summarize()
	}
	for _, m := range r.statuses {
		for status, n := range m {
			res.Requests += n
			switch {
			case status == 0:
				res.Unreachable += n
			case status == 429:
				res.Refused += n
			case status/100 != 2 && status != 101:
				res.Failed += n
			}
		}
	}
	return res
}
