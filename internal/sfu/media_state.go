package sfu

import (
	"bytes"
	"slices"
	"sync"

	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
)

// MediaState tells which video a user currently publishes. Viewers learn this
// without receiving the media, so they can offer to watch a screen share.
type MediaState struct {
	Screen bool
	Camera bool
}

// mediaNotifier is shared by all rooms of an SFU.
type mediaNotifier struct {
	mu      sync.RWMutex
	fn      func(roomID, userID uuid.UUID, state MediaState)
	viewers func(roomID, sharer uuid.UUID, viewers []uuid.UUID)
	layers  func(roomID, sharer uuid.UUID, layers []Layer)
	quality func(roomID, viewer, sharer uuid.UUID, state QualityState)
	wanted  func(roomID, sharer uuid.UUID, layers []int)
}

func (n *mediaNotifier) setLayers(fn func(roomID, sharer uuid.UUID, layers []Layer)) {
	n.mu.Lock()
	n.layers = fn
	n.mu.Unlock()
}

func (n *mediaNotifier) setQuality(fn func(roomID, viewer, sharer uuid.UUID, state QualityState)) {
	n.mu.Lock()
	n.quality = fn
	n.mu.Unlock()
}

func (n *mediaNotifier) setWanted(fn func(roomID, sharer uuid.UUID, layers []int)) {
	n.mu.Lock()
	n.wanted = fn
	n.mu.Unlock()
}

func (n *mediaNotifier) layersFn() func(roomID, sharer uuid.UUID, layers []Layer) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.layers
}

func (n *mediaNotifier) qualityFn() func(roomID, viewer, sharer uuid.UUID, state QualityState) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.quality
}

func (n *mediaNotifier) wantedFn() func(roomID, sharer uuid.UUID, layers []int) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.wanted
}

func (n *mediaNotifier) set(fn func(roomID, userID uuid.UUID, state MediaState)) {
	n.mu.Lock()
	n.fn = fn
	n.mu.Unlock()
}

func (n *mediaNotifier) setViewers(fn func(roomID, sharer uuid.UUID, viewers []uuid.UUID)) {
	n.mu.Lock()
	n.viewers = fn
	n.mu.Unlock()
}

func (n *mediaNotifier) get() (func(roomID, userID uuid.UUID, state MediaState), func(roomID, sharer uuid.UUID, viewers []uuid.UUID)) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.fn, n.viewers
}

// MediaStates returns the users that currently publish a screen or camera.
func (r *Room) MediaStates() map[uuid.UUID]MediaState {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.mediaStatesLocked()
}

func (r *Room) mediaStatesLocked() map[uuid.UUID]MediaState {
	out := map[uuid.UUID]MediaState{}
	for _, info := range r.trackLocals {
		if info.Kind != webrtc.RTPCodecTypeVideo {
			continue
		}
		st := out[info.SenderID]
		switch info.Source {
		case SourceScreen:
			st.Screen = true
		case SourceCamera:
			st.Camera = true
		}
		out[info.SenderID] = st
	}
	return out
}

// ScreenViewers returns every live screen share in the room with the users
// watching it (see screenViewersLocked). A share nobody watches maps to an
// empty list.
func (r *Room) ScreenViewers() map[uuid.UUID][]uuid.UUID {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.screenViewersLocked(r.mediaStatesLocked())
}

// screenViewersLocked maps each user with a live screen share (per states) to
// the users in the room subscribed to it: distinct users with a peer here,
// never the sharer, sorted. r.mu must be held.
func (r *Room) screenViewersLocked(states map[uuid.UUID]MediaState) map[uuid.UUID][]uuid.UUID {
	out := map[uuid.UUID][]uuid.UUID{}
	for sharer, st := range states {
		if !st.Screen {
			continue
		}
		viewers := []uuid.UUID{}
		for viewer, s := range r.subs {
			// A subscription without a peer (failed join) receives nothing.
			if viewer == sharer || r.peers[viewer] == nil || !s.screens[sharer] {
				continue
			}
			viewers = append(viewers, viewer)
		}
		slices.SortFunc(viewers, compareIDs)
		out[sharer] = viewers
	}
	return out
}

// compareIDs orders user IDs like their canonical string form.
func compareIDs(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) }

// notifyMedia announces changed screen/camera availability and changed
// screen-share audiences. It runs at the start of every signaling round,
// which every track, peer and subscription change triggers anyway. notifyMu
// keeps the announcements in the order the states were computed.
func (r *Room) notifyMedia() {
	mediaFn, viewersFn := r.notifier.get()
	layersFn := r.notifier.layersFn()
	if mediaFn == nil && viewersFn == nil && layersFn == nil {
		return
	}
	r.notifyMu.Lock()
	defer r.notifyMu.Unlock()

	r.mu.Lock()
	now := r.mediaStatesLocked()
	type change struct {
		user  uuid.UUID
		state MediaState
	}
	var changes []change
	if mediaFn != nil {
		for u, st := range now {
			if r.lastMedia[u] != st {
				changes = append(changes, change{u, st})
			}
		}
		for u := range r.lastMedia {
			if _, ok := now[u]; !ok {
				changes = append(changes, change{u, MediaState{}})
			}
		}
		r.lastMedia = now
	}
	type audience struct {
		sharer  uuid.UUID
		viewers []uuid.UUID
	}
	var audiences []audience
	if viewersFn != nil {
		// lastViewers keeps only non-empty audiences: an absent sharer was
		// last announced (or implied) as watched by nobody.
		next := map[uuid.UUID][]uuid.UUID{}
		for sharer, viewers := range r.screenViewersLocked(now) {
			if len(viewers) == 0 {
				continue
			}
			next[sharer] = viewers
			if !slices.Equal(r.lastViewers[sharer], viewers) {
				audiences = append(audiences, audience{sharer, slices.Clone(viewers)})
			}
		}
		for sharer := range r.lastViewers {
			if _, ok := next[sharer]; !ok {
				audiences = append(audiences, audience{sharer, []uuid.UUID{}})
			}
		}
		r.lastViewers = next
	}
	type layerChange struct {
		sharer uuid.UUID
		layers []Layer
	}
	var layerChanges []layerChange
	if layersFn != nil {
		next := r.screenLayersLocked(now)
		for sharer, layers := range next {
			if !slices.Equal(r.lastLayers[sharer], layers) {
				layerChanges = append(layerChanges, layerChange{sharer, layers})
			}
		}
		for sharer := range r.lastLayers {
			if _, ok := next[sharer]; !ok {
				layerChanges = append(layerChanges, layerChange{sharer, []Layer{}})
			}
		}
		r.lastLayers = next
	}
	r.mu.Unlock()

	for _, c := range changes {
		mediaFn(r.ID, c.user, c.state)
	}
	slices.SortFunc(layerChanges, func(a, b layerChange) int { return compareIDs(a.sharer, b.sharer) })
	for _, c := range layerChanges {
		layersFn(r.ID, c.sharer, c.layers)
	}
	slices.SortFunc(audiences, func(a, b audience) int { return compareIDs(a.sharer, b.sharer) })
	for _, a := range audiences {
		viewersFn(r.ID, a.sharer, a.viewers)
	}
}
