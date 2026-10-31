package main

import (
	"fmt"
	"strconv"
	"strings"
)

// cameraView says which of the published cameras a member keeps visible
// (WATCH_CAMERAS). The server sends every camera by default and subscriptions
// work by opting out, so showing only some means hiding the others one by one.
type cameraView struct {
	limit int // -1: all (the default), 0: none, n: n cameras
}

var allCameras = cameraView{limit: -1}

// parseCameraView reads WATCH_CAMERAS: empty, "all", "true", "yes" show every
// camera, "none" or "0" hide them all, a number n shows n of them.
func parseCameraView(s string) (cameraView, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "all", "true", "yes":
		return allCameras, nil
	case "none", "false":
		return cameraView{}, nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 0 {
		return allCameras, fmt.Errorf("WATCH_CAMERAS=%q: use all, none or a number of cameras", s)
	}
	return cameraView{limit: n}, nil
}

// cameraPlan is the opt-out one member sends.
type cameraPlan struct {
	allOff bool     // hide every camera
	hide   []string // cameras to hide one by one
}

// plan decides what the member at position pos (self is its user id) hides.
// Each member shows the n cameras that follow its own position in the ring of
// publishers, so the shown cameras spread over everybody like the tiles of
// different people, instead of all members watching the same first n.
func (v cameraView) plan(self string, pos int, cameras []string) cameraPlan {
	if v.limit < 0 {
		return cameraPlan{}
	}
	if v.limit == 0 {
		return cameraPlan{allOff: true}
	}
	var others []string
	for _, id := range cameras {
		if id != self {
			others = append(others, id)
		}
	}
	if v.limit >= len(others) {
		return cameraPlan{}
	}
	shown := map[string]bool{}
	for j := range v.limit {
		shown[others[(pos+j)%len(others)]] = true
	}
	var p cameraPlan
	for _, id := range others {
		if !shown[id] {
			p.hide = append(p.hide, id)
		}
	}
	return p
}

// watchers counts, for every camera, the members that show it. members are the
// user ids of the whole slice in order; only those in alive (all when nil)
// count as watching.
func (v cameraView) watchers(members, cameras []string, alive map[string]bool) map[string]int {
	out := map[string]int{}
	for pos, self := range members {
		if alive != nil && !alive[self] {
			continue
		}
		p := v.plan(self, pos, cameras)
		if p.allOff {
			continue
		}
		hidden := map[string]bool{}
		for _, id := range p.hide {
			hidden[id] = true
		}
		for _, id := range cameras {
			if id != self && !hidden[id] {
				out[id]++
			}
		}
	}
	return out
}
