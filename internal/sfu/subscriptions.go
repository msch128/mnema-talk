package sfu

import (
	"errors"

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

// Subscriptions is what one viewer wants to receive. Voices are always
// forwarded. Cameras are on by default and the viewer opts out per publisher
// (or for everyone); screen shares, with their sound, are off by default and
// the viewer opts in.
type Subscriptions struct {
	allCamerasOff bool
	hiddenCameras map[uuid.UUID]bool
	screens       map[uuid.UUID]bool
	// quality is the layer chosen per publisher's screen share; absent means
	// automatic.
	quality map[uuid.UUID]int8
	// fit is the best layer the viewer's picture of a screen share can use;
	// absent means the best the share has.
	fit map[uuid.UUID]int8
	// cameraPixels is how many device pixels tall the viewer's picture of a
	// publisher's camera is; absent means unknown (the best layer).
	cameraPixels map[uuid.UUID]int32
}

// screenQuality is the layer chosen for publisher's screen share, -1 for
// automatic.
func (s *Subscriptions) screenQuality(publisher uuid.UUID) int {
	if s == nil {
		return -1
	}
	if q, ok := s.quality[publisher]; ok {
		return int(q)
	}
	return -1
}

// setScreenQuality remembers the viewer's layer choice for a screen share
// (-1 forgets it). It is kept even before the share is watched, so the
// choice made with "Watch" applies from the first picture.
func (s *Subscriptions) setScreenQuality(publisher uuid.UUID, layer int) {
	if layer < 0 {
		delete(s.quality, publisher)
		return
	}
	if _, ok := s.quality[publisher]; !ok && len(s.quality) >= maxScreenSubs {
		return
	}
	if s.quality == nil {
		s.quality = map[uuid.UUID]int8{}
	}
	s.quality[publisher] = int8(layer)
}

// screenFit is the best layer of publisher's screen share the viewer can use.
func (s *Subscriptions) screenFit(publisher uuid.UUID) int {
	if s == nil {
		return 0
	}
	return int(s.fit[publisher])
}

// setScreenFit remembers the best layer of a screen share the viewer's
// picture can use.
func (s *Subscriptions) setScreenFit(publisher uuid.UUID, layer int) {
	if layer <= 0 {
		delete(s.fit, publisher)
		return
	}
	if _, ok := s.fit[publisher]; !ok && len(s.fit) >= maxScreenSubs {
		return
	}
	if s.fit == nil {
		s.fit = map[uuid.UUID]int8{}
	}
	s.fit[publisher] = int8(min(layer, maxLayers-1))
}

// cameraFit is the height in device pixels of the viewer's picture of
// publisher's camera, 0 when unknown.
func (s *Subscriptions) cameraFit(publisher uuid.UUID) int {
	if s == nil {
		return 0
	}
	return int(s.cameraPixels[publisher])
}

// setCameraFit remembers the height of the viewer's picture of a camera.
func (s *Subscriptions) setCameraFit(publisher uuid.UUID, pixels int) {
	if pixels <= 0 {
		delete(s.cameraPixels, publisher)
		return
	}
	if _, ok := s.cameraPixels[publisher]; !ok && len(s.cameraPixels) >= maxHiddenCameras {
		return
	}
	if s.cameraPixels == nil {
		s.cameraPixels = map[uuid.UUID]int32{}
	}
	s.cameraPixels[publisher] = int32(pixels)
}

// Wants reports whether a track of the given kind and source published by
// sender should be forwarded to this viewer.
func (s *Subscriptions) Wants(sender uuid.UUID, kind webrtc.RTPCodecType, source Source) bool {
	if kind != webrtc.RTPCodecTypeVideo {
		if source == SourceScreenAudio {
			return s != nil && s.screens[sender]
		}
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
func (s *Subscriptions) dropScreen(publisher uuid.UUID) {
	delete(s.screens, publisher)
	delete(s.quality, publisher)
}

// dropCameraFit forgets the size of the viewer's picture of a camera that is gone.
func (s *Subscriptions) dropCameraFit(publisher uuid.UUID) {
	delete(s.cameraPixels, publisher)
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

func applySubscription(s *Subscriptions, publisher uuid.UUID, kind Source, all, on bool) error {
	switch {
	case kind == SourceCamera && all:
		s.SetAllCameras(on)
		return nil
	case kind == SourceCamera:
		return s.SetCamera(publisher, on)
	case kind == SourceScreen:
		return s.SetScreen(publisher, on)
	default:
		return errors.New("unknown subscription kind")
	}
}

func keyframeWanted(info *TrackInfo, kind Source, all bool, publisher uuid.UUID) bool {
	if info.Kind != webrtc.RTPCodecTypeVideo || info.group != nil {
		return false
	}
	if info.Source != kind {
		return false
	}
	if all {
		return true
	}
	return info.SenderID == publisher
}

// A new viewer of a stream needs a keyframe to start decoding. A layered
// share is asked for when the viewer's track exists. r.mu must be held.
func (r *Room) keyframeTracks(failed error, on bool, viewer, publisher uuid.UUID, kind Source, all bool) []*TrackInfo {
	if failed != nil || !on {
		return nil
	}
	var fresh []*TrackInfo
	for _, info := range r.trackLocals {
		if keyframeWanted(info, kind, all, publisher) && r.wantsLocked(viewer, info) {
			fresh = append(fresh, info)
		}
	}
	return fresh
}

// Subscribe changes what viewer receives. kind is SourceScreen or
// SourceCamera. all (cameras only) applies to every publisher. Tracks are
// added or removed by renegotiating with the viewer.
func (r *Room) Subscribe(viewer, publisher uuid.UUID, kind Source, all, on bool) error {
	r.mu.Lock()
	if r.peers[viewer] == nil {
		r.mu.Unlock()
		return ErrNotInRoom
	}
	err := applySubscription(r.subsLocked(viewer), publisher, kind, all, on)
	fresh := r.keyframeTracks(err, on, viewer, publisher, kind, all)
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
