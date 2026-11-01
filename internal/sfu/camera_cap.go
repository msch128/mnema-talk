package sfu

import (
	"slices"

	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
)

// MaxViewerCameras is the most camera streams the server forwards to one
// viewer at a time. A client that only asks for the cameras it shows never
// gets near it (a grid of 5 x 5 tiles fits); it is the safety net for one that
// subscribes to everything in a very large Talk, so one viewer cannot make the
// server send hundreds of pictures. Cameras beyond it are not forwarded; the
// viewer is told which (see SetCamerasCappedHandler) and shows a placeholder.
// Which cameras fall outside is decided by user ID, so it does not change
// while the same people are in the Talk; it does not look at what the viewer
// focuses, so a camera on the stage can fall outside (accepted for a limit
// meant never to be reached).
const MaxViewerCameras = 24

// capCameras removes from desired the camera tracks beyond MaxViewerCameras
// and returns whose they were, sorted. desired holds what the viewer asked
// for already; nothing is returned while it fits.
func capCameras(desired map[string]*TrackInfo) []uuid.UUID {
	var senders []uuid.UUID
	for _, info := range desired {
		if isCameraVideo(info) {
			senders = append(senders, info.SenderID)
		}
	}
	// One camera per publisher counts, however many tracks it came as.
	slices.SortFunc(senders, compareIDs)
	senders = slices.Compact(senders)
	if len(senders) <= MaxViewerCameras {
		return nil
	}
	capped := senders[MaxViewerCameras:]
	for id, info := range desired {
		if isCameraVideo(info) && slices.Contains(capped, info.SenderID) {
			delete(desired, id)
		}
	}
	return capped
}

func isCameraVideo(info *TrackInfo) bool {
	return info.Kind == webrtc.RTPCodecTypeVideo && info.Source == SourceCamera
}

// SetCamerasCappedHandler registers the callback that tells a viewer whose
// cameras the server does not forward because of MaxViewerCameras. It runs
// whenever that set changes, with an empty list once nothing is capped.
func (s *SFU) SetCamerasCappedHandler(fn func(roomID, viewer uuid.UUID, publishers []uuid.UUID)) {
	s.notifier.mu.Lock()
	s.notifier.capped = fn
	s.notifier.mu.Unlock()
}

func (n *mediaNotifier) cappedFn() func(roomID, viewer uuid.UUID, publishers []uuid.UUID) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	return n.capped
}

// announceCapped tells the peer whose cameras are capped when that changed
// since the last round. The caller owns peer.signalingMu.
func (r *Room) announceCapped(peer *Peer, capped []uuid.UUID) {
	if slices.Equal(peer.lastCapped, capped) {
		return
	}
	peer.lastCapped = slices.Clone(capped)
	if fn := r.notifier.cappedFn(); fn != nil {
		fn(r.ID, peer.ID, slices.Clone(capped))
	}
}

// SubscribeCameras turns the cameras of several publishers on or off for
// viewer at once, with one renegotiation of that viewer's connection instead
// of one per camera. Publishers beyond the viewer's limit of remembered
// choices are ignored with ErrTooManySubscriptions after the rest was applied.
func (r *Room) SubscribeCameras(viewer uuid.UUID, publishers []uuid.UUID, on bool) error {
	r.mu.Lock()
	peer := r.peers[viewer]
	if peer == nil {
		r.mu.Unlock()
		return ErrNotInRoom
	}
	subs := r.subsLocked(viewer)
	var failed error
	for _, publisher := range publishers {
		if err := subs.SetCamera(publisher, on); err != nil {
			failed = err
		}
	}
	var fresh []*TrackInfo
	if on {
		for _, publisher := range publishers {
			fresh = append(fresh, r.keyframeTracks(nil, true, viewer, publisher, SourceCamera, false)...)
		}
	}
	r.mu.Unlock()
	for _, info := range fresh {
		info.requestKeyframe()
	}
	go r.requestPeerSignal(peer)
	return failed
}
