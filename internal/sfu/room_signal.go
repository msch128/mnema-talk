package sfu

import (
	"log/slog"
	"time"

	"github.com/pion/webrtc/v4"
)

// SetAnswer applies the client's answer and runs a pending renegotiation.
// An answer that cannot be applied leaves the offer outstanding, so its
// watchdog sends it again after the room's offerTimeout.
func (p *Peer) SetAnswer(answer webrtc.SessionDescription) error {
	if err := p.PC.SetRemoteDescription(answer); err != nil {
		return err
	}
	p.applyEarlyCandidates()
	if p.negotiationPending.Swap(false) {
		p.room.requestPeerSignal(p)
	}
	return nil
}

func (r *Room) SignalPeerConnections() {
	r.notifyMedia()
	r.mu.RLock()
	peers := make([]*Peer, 0, len(r.peers))
	for _, peer := range r.peers {
		peers = append(peers, peer)
	}
	r.mu.RUnlock()
	for _, peer := range peers {
		r.requestPeerSignal(peer)
	}
}

// requestPeerSignal coalesces changes behind one peer's active task. Completion
// and answer retries concern only that peer: repeating a whole-room scan would
// mark unrelated busy peers pending again and amplify a signaling burst.
func (r *Room) requestPeerSignal(peer *Peer) {
	// Publish the request before testing task admission. Otherwise a task can
	// finish before a failed TryLock caller stores its request, losing a change.
	peer.negotiationPending.Store(true)
	if !peer.signalingMu.TryLock() {
		return
	}
	go func() {
		defer func() {
			peer.signalingMu.Unlock()
			if peer.PC.SignalingState() == webrtc.SignalingStateStable && peer.negotiationPending.Swap(false) {
				r.requestPeerSignal(peer)
			}
		}()
		r.signalPeer(peer)
	}()
}

// signalPeer operates on a membership snapshot. No PeerConnection/network
// operation runs under the room lock, and a blocked peer cannot hold up others.
// The caller owns peer.signalingMu.
func (r *Room) signalPeer(peer *Peer) {
	// Consume requests before taking the membership snapshot. Any subsequent
	// request stays pending for the completion/answer path; earlier requests
	// are reflected in the snapshot below.
	peer.negotiationPending.Store(false)
	desired, ok := r.desiredTracks(peer)
	if !ok {
		return
	}
	if peer.PC.ConnectionState() == webrtc.PeerConnectionStateClosed {
		return
	}
	if peer.PC.SignalingState() != webrtc.SignalingStateStable {
		peer.negotiationPending.Store(true)
		return
	}
	existing, removed := removeStaleSenders(peer, desired)
	added := r.addMissingSenders(peer, desired, existing)
	if removed || added || !peer.offerSent {
		negotiateOffer(peer)
	}
}

// desiredTracks returns the tracks the peer should receive, keyed by the ID
// the viewer sees. ok is false when the peer is no longer in the room.
func (r *Room) desiredTracks(peer *Peer) (desired map[string]*TrackInfo, ok bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.peers[peer.ID] != peer {
		return nil, false
	}
	desired = make(map[string]*TrackInfo)
	for id, info := range r.trackLocals {
		if !r.wantsLocked(peer.ID, info) {
			continue
		}
		// The layers of a screen share reach a viewer as one track.
		if info.group != nil {
			id = info.group.id
			if prev, ok := desired[id]; ok && prev.layer < info.layer {
				continue
			}
		}
		desired[id] = info
	}
	return desired, true
}

// removeStaleSenders drops the senders whose track the peer no longer wants.
// It returns the track IDs still sent and whether any sender was removed.
func removeStaleSenders(peer *Peer, desired map[string]*TrackInfo) (existing map[string]bool, removed bool) {
	existing = make(map[string]bool)
	for _, sender := range peer.PC.GetSenders() {
		if sender.Track() == nil {
			continue
		}
		trackID := sender.Track().ID()
		existing[trackID] = true
		if _, ok := desired[trackID]; ok {
			continue
		}
		if err := peer.PC.RemoveTrack(sender); err != nil {
			continue
		}
		removed = true
		if vt := peer.takeViewerTrack(trackID); vt != nil {
			vt.group.removeViewer(vt)
		}
	}
	return existing, removed
}

// addMissingSenders adds every desired track the peer is not yet sent and
// reports whether it added any.
func (r *Room) addMissingSenders(peer *Peer, desired map[string]*TrackInfo, existing map[string]bool) bool {
	added := false
	for trackID, info := range desired {
		if existing[trackID] {
			continue
		}
		if r.addSender(peer, trackID, info) {
			added = true
		}
	}
	return added
}

// addSender adds one track to the peer and starts its RTCP and keyframe
// handling. It reports whether the track was added.
func (r *Room) addSender(peer *Peer, trackID string, info *TrackInfo) bool {
	recipientTrack, vt := r.recipientTrack(peer, trackID, info)
	sender, err := peer.PC.AddTrack(recipientTrack)
	if err != nil {
		// E.g. the viewer's browser cannot receive the publisher's codec:
		// it then gets no media from this track.
		peer.noteAddTrackFailure(trackID, info, err)
		return false
	}
	delete(peer.addTrackFailed, trackID)
	if vt != nil {
		peer.putViewerTrack(trackID, vt)
		// The new viewer waits for a keyframe of its layer, which
		// addViewer asks for.
		info.group.addViewer(vt)
		go r.forwardRTCP(sender, vt.viewerKeyframe, vt.observeReportWithBandwidth)
		return true
	}
	go r.forwardRTCP(sender, info.requestKeyframe, nil)
	if info.Kind == webrtc.RTPCodecTypeVideo {
		// A new viewer needs a keyframe to start decoding.
		info.requestKeyframe()
	}
	return true
}

// recipientTrack picks what the peer's PeerConnection gets for info: the
// shared track, a per-viewer layer switcher for screen shares, and an
// authorization wrapper when either end requires media authorization.
func (r *Room) recipientTrack(peer *Peer, trackID string, info *TrackInfo) (webrtc.TrackLocal, *viewerTrack) {
	var recipientTrack webrtc.TrackLocal = info.Track
	var vt *viewerTrack
	if info.group != nil {
		r.mu.RLock()
		pref := r.subs[peer.ID].screenQuality(info.SenderID)
		r.mu.RUnlock()
		vt = info.group.newViewer(peer.ID, pref, trackID, info.SenderID.String())
		recipientTrack = vt
	}
	if peer.authorizeMedia != nil || (info.publisher != nil && info.publisher.authorizeMedia != nil) {
		var source func() bool
		if info.publisher != nil && info.publisher.authorizeMedia != nil {
			source = info.publisher.mediaAuthorized
		}
		recipientTrack = &authorizedTrack{TrackLocal: recipientTrack, authorize: peer.mediaAuthorized, source: source, sources: r.sourceAuthorization}
	}
	return recipientTrack, vt
}

// noteAddTrackFailure logs the first failure to add a track and remembers it,
// so a track the browser cannot receive does not log on every round.
func (p *Peer) noteAddTrackFailure(trackID string, info *TrackInfo, err error) {
	if p.addTrackFailed[trackID] {
		return
	}
	if p.addTrackFailed == nil {
		p.addTrackFailed = make(map[string]bool)
	}
	p.addTrackFailed[trackID] = true
	slog.Warn("sfu add track", "viewer", p.ID, "publisher", info.SenderID, "source", string(info.Source),
		"codec", info.codecMime(), "err", err)
}

// negotiateOffer creates and sets the local offer and hands it to the client.
func negotiateOffer(peer *Peer) {
	offer, err := peer.PC.CreateOffer(nil)
	if err != nil {
		slog.Error("sfu create offer", "err", err)
		return
	}
	if err := peer.PC.SetLocalDescription(offer); err != nil {
		slog.Error("sfu set local description", "err", err)
		return
	}
	peer.offerSent = true
	gen := peer.offerGen.Add(1)
	peer.watchOffer(gen, 1)
	if peer.SendOffer != nil {
		peer.SendOffer(offer)
	}
	// Gathering started with SetLocalDescription, so candidates may be
	// waiting; the client must see them after the offer.
	peer.offerDelivered()
}

// watchOffer arms the watchdog for offer gen: if it is still unanswered
// after the room's offerTimeout, the same offer goes out again.
func (p *Peer) watchOffer(gen uint64, attempt int) {
	time.AfterFunc(p.room.offerTimeout, func() {
		p.signalingMu.Lock()
		defer func() {
			p.signalingMu.Unlock()
			// An answer may stabilize the peer while the resend callback is
			// still running. Drain changes queued behind this task as well.
			if p.PC.SignalingState() == webrtc.SignalingStateStable && p.negotiationPending.Swap(false) {
				p.room.requestPeerSignal(p)
			}
		}()
		if p.offerGen.Load() != gen || p.PC.ConnectionState() == webrtc.PeerConnectionStateClosed ||
			p.PC.SignalingState() != webrtc.SignalingStateHaveLocalOffer {
			return
		}
		pending := p.PC.PendingLocalDescription()
		if pending == nil {
			return
		}
		if attempt > maxOfferResends {
			slog.Error("sfu offer never answered, giving up", "user", p.ID, "resends", maxOfferResends)
			return
		}
		slog.Warn("sfu offer unanswered, sending it again", "user", p.ID, "attempt", attempt)
		if p.SendOffer != nil {
			p.SendOffer(*pending)
		}
		p.watchOffer(gen, attempt+1)
	})
}
