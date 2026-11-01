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
	// capped says whose cameras a viewer is not sent (see camera_cap.go).
	capped func(roomID, viewer uuid.UUID, publishers []uuid.UUID)
	// wantedCamera is wanted for the layers of a camera.
	wantedCamera func(roomID, sharer uuid.UUID, layers []int)
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

func (n *mediaNotifier) setWantedCamera(fn func(roomID, sharer uuid.UUID, layers []int)) {
	n.mu.Lock()
	n.wantedCamera = fn
	n.mu.Unlock()
}

// wantedFnFor is the callback that announces the wanted layers of source.
func (n *mediaNotifier) wantedFnFor(source Source) func(roomID, sharer uuid.UUID, layers []int) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	if source == SourceCamera {
		return n.wantedCamera
	}
	return n.wanted
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

func (n *mediaNotifier) hasListeners() bool {
	mediaFn, viewersFn := n.get()
	return mediaFn != nil || viewersFn != nil || n.layersFn() != nil
}

// compareIDs orders user IDs like their canonical string form.
func compareIDs(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) }

// notifyMedia announces changed screen/camera availability and changed
// screen-share audiences. It runs at the start of every signaling round,
// which every track, peer and subscription change triggers anyway. notifyMu
// keeps the announcements in the order the states were computed.
func (r *Room) notifyMedia() {
	if !r.notifier.hasListeners() {
		return
	}
	mediaFn, viewersFn := r.notifier.get()
	layersFn := r.notifier.layersFn()
	r.notifyMu.Lock()
	defer r.notifyMu.Unlock()

	r.mu.Lock()
	pending := r.collectMediaNotificationsLocked(mediaFn != nil, viewersFn != nil, layersFn != nil)
	r.mu.Unlock()

	r.dispatchMediaNotifications(pending, mediaFn, viewersFn, layersFn)
}

type pendingMediaNotifications struct {
	changes      []mediaChange
	audiences    []audienceChange
	layerChanges []layerChange
}

func (r *Room) collectMediaNotificationsLocked(needMedia, needViewers, needLayers bool) pendingMediaNotifications {
	now := r.mediaStatesLocked()
	var p pendingMediaNotifications
	if needMedia {
		p.changes = r.takeMediaChangesLocked(now)
	}
	if needViewers {
		p.audiences = r.takeAudienceChangesLocked(now)
	}
	if needLayers {
		p.layerChanges = r.takeLayerChangesLocked(now)
	}
	return p
}

func (r *Room) dispatchMediaNotifications(p pendingMediaNotifications, mediaFn func(uuid.UUID, uuid.UUID, MediaState), viewersFn func(uuid.UUID, uuid.UUID, []uuid.UUID), layersFn func(uuid.UUID, uuid.UUID, []Layer)) {
	if mediaFn != nil {
		for _, c := range p.changes {
			mediaFn(r.ID, c.user, c.state)
		}
	}
	if layersFn != nil {
		slices.SortFunc(p.layerChanges, func(a, b layerChange) int { return compareIDs(a.sharer, b.sharer) })
		for _, c := range p.layerChanges {
			layersFn(r.ID, c.sharer, c.layers)
		}
	}
	if viewersFn != nil {
		slices.SortFunc(p.audiences, func(a, b audienceChange) int { return compareIDs(a.sharer, b.sharer) })
		for _, a := range p.audiences {
			viewersFn(r.ID, a.sharer, a.viewers)
		}
	}
}

type mediaChange struct {
	user  uuid.UUID
	state MediaState
}

type audienceChange struct {
	sharer  uuid.UUID
	viewers []uuid.UUID
}

type layerChange struct {
	sharer uuid.UUID
	layers []Layer
}

// takeMediaChangesLocked returns the users whose screen/camera state differs
// from the last announcement (departed users as an empty state) and records
// now as announced. r.mu is held.
func (r *Room) takeMediaChangesLocked(now map[uuid.UUID]MediaState) []mediaChange {
	var changes []mediaChange
	for u, st := range now {
		if r.lastMedia[u] != st {
			changes = append(changes, mediaChange{u, st})
		}
	}
	for u := range r.lastMedia {
		if _, ok := now[u]; !ok {
			changes = append(changes, mediaChange{u, MediaState{}})
		}
	}
	r.lastMedia = now
	return changes
}

// takeAudienceChangesLocked returns the screen shares whose audience differs
// from the last announcement and records the current audiences as announced.
// r.mu is held.
func (r *Room) takeAudienceChangesLocked(now map[uuid.UUID]MediaState) []audienceChange {
	var audiences []audienceChange
	// lastViewers keeps only non-empty audiences: an absent sharer was
	// last announced (or implied) as watched by nobody.
	next := map[uuid.UUID][]uuid.UUID{}
	for sharer, viewers := range r.screenViewersLocked(now) {
		if len(viewers) == 0 {
			continue
		}
		next[sharer] = viewers
		if !slices.Equal(r.lastViewers[sharer], viewers) {
			audiences = append(audiences, audienceChange{sharer, slices.Clone(viewers)})
		}
	}
	for sharer := range r.lastViewers {
		if _, ok := next[sharer]; !ok {
			audiences = append(audiences, audienceChange{sharer, []uuid.UUID{}})
		}
	}
	r.lastViewers = next
	return audiences
}

// takeLayerChangesLocked returns the screen shares whose offered layers differ
// from the last announcement and records the current layers as announced.
// r.mu is held.
func (r *Room) takeLayerChangesLocked(now map[uuid.UUID]MediaState) []layerChange {
	var changes []layerChange
	next := r.screenLayersLocked(now)
	for sharer, layers := range next {
		if !slices.Equal(r.lastLayers[sharer], layers) {
			changes = append(changes, layerChange{sharer, layers})
		}
	}
	for sharer := range r.lastLayers {
		if _, ok := next[sharer]; !ok {
			changes = append(changes, layerChange{sharer, []Layer{}})
		}
	}
	r.lastLayers = next
	return changes
}
