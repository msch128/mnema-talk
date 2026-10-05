package sfu

import (
	"bytes"
	"errors"
	"slices"
	"sync"

	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
)

// Limits on what one viewer may remember, so a client cannot grow the
// server's memory with made-up IDs.
const (
	maxHiddenCameras = 512
	maxScreenSubs    = 64
)

// ErrTooManySubscriptions is returned when a viewer exceeds the per-viewer limits.
var ErrTooManySubscriptions = errors.New("too many subscriptions")

// Subscriptions is what one viewer wants to receive. Audio is always
// forwarded. Cameras are on by default and the viewer opts out per publisher
// (or for everyone); screen shares are off by default and the viewer opts in.
type Subscriptions struct {
	allCamerasOff bool
	hiddenCameras map[uuid.UUID]bool
	screens       map[uuid.UUID]bool
}

// Wants reports whether a track of the given kind and source published by
// sender should be forwarded to this viewer.
func (s *Subscriptions) Wants(sender uuid.UUID, kind webrtc.RTPCodecType, source Source) bool {
	if kind != webrtc.RTPCodecTypeVideo {
		return true
	}
	if s == nil {
		return source == SourceCamera
	}
	switch source {
	case SourceCamera:
		return !s.allCamerasOff && !s.hiddenCameras[sender]
	case SourceScreen:
		return s.screens[sender]
	}
	return false
}

// SetCamera turns publisher's camera on or off for this viewer.
func (s *Subscriptions) SetCamera(publisher uuid.UUID, on bool) error {
	if on {
		delete(s.hiddenCameras, publisher)
		return nil
	}
	if s.hiddenCameras == nil {
		s.hiddenCameras = map[uuid.UUID]bool{}
	}
	if !s.hiddenCameras[publisher] && len(s.hiddenCameras) >= maxHiddenCameras {
		return ErrTooManySubscriptions
	}
	s.hiddenCameras[publisher] = true
	return nil
}

// SetAllCameras turns every camera off (or back to the per-person choices).
func (s *Subscriptions) SetAllCameras(on bool) { s.allCamerasOff = !on }

// SetScreen subscribes to or unsubscribes from publisher's screen share.
func (s *Subscriptions) SetScreen(publisher uuid.UUID, on bool) error {
	if !on {
		delete(s.screens, publisher)
		return nil
	}
	if s.screens == nil {
		s.screens = map[uuid.UUID]bool{}
	}
	if !s.screens[publisher] && len(s.screens) >= maxScreenSubs {
		return ErrTooManySubscriptions
	}
	s.screens[publisher] = true
	return nil
}

// dropScreen forgets a publisher's screen opt-in: the opt-in belongs to one share.
func (s *Subscriptions) dropScreen(publisher uuid.UUID) { delete(s.screens, publisher) }

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

// subsLocked returns the viewer's subscriptions, creating them. r.mu must be held for writing.
func (r *Room) subsLocked(viewer uuid.UUID) *Subscriptions {
	s := r.subs[viewer]
	if s == nil {
		s = &Subscriptions{}
		r.subs[viewer] = s
	}
	return s
}

// wantsLocked reports whether viewer should receive info. r.mu must be held.
func (r *Room) wantsLocked(viewer uuid.UUID, info *TrackInfo) bool {
	if info.SenderID == viewer {
		return false
	}
	return r.subs[viewer].Wants(info.SenderID, info.Kind, info.Source)
}

// Subscribe changes what viewer receives. kind is SourceScreen or
// SourceCamera. all (cameras only) applies to every publisher. Tracks are
// added or removed by renegotiating with the viewer.
func (r *Room) Subscribe(viewer, publisher uuid.UUID, kind Source, all, on bool) error {
	r.mu.Lock()
	s := r.subsLocked(viewer)
	var err error
	switch {
	case kind == SourceCamera && all:
		s.SetAllCameras(on)
	case kind == SourceCamera:
		err = s.SetCamera(publisher, on)
	case kind == SourceScreen:
		err = s.SetScreen(publisher, on)
	default:
		err = errors.New("unknown subscription kind")
	}
	// A new viewer of a stream needs a keyframe to start decoding.
	var fresh []*TrackInfo
	if err == nil && on {
		for _, info := range r.trackLocals {
			if info.Kind == webrtc.RTPCodecTypeVideo && info.Source == kind && (all || info.SenderID == publisher) && r.wantsLocked(viewer, info) {
				fresh = append(fresh, info)
			}
		}
	}
	r.mu.Unlock()
	if err != nil {
		return err
	}
	for _, info := range fresh {
		info.requestKeyframe()
	}
	go r.SignalPeerConnections()
	return nil
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
	if mediaFn == nil && viewersFn == nil {
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
	r.mu.Unlock()

	for _, c := range changes {
		mediaFn(r.ID, c.user, c.state)
	}
	slices.SortFunc(audiences, func(a, b audience) int { return compareIDs(a.sharer, b.sharer) })
	for _, a := range audiences {
		viewersFn(r.ID, a.sharer, a.viewers)
	}
}
