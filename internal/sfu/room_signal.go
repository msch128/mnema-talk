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
	r.mu.RLock()
	if r.peers[peer.ID] != peer {
		r.mu.RUnlock()
		return
	}
	desired := make(map[string]*TrackInfo)
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
	r.mu.RUnlock()
	if peer.PC.ConnectionState() == webrtc.PeerConnectionStateClosed {
		return
	}
	if peer.PC.SignalingState() != webrtc.SignalingStateStable {
		peer.negotiationPending.Store(true)
		return
	}
	existingSenders := make(map[string]bool)
	needRenegotiate := false
	for _, sender := range peer.PC.GetSenders() {
		if sender.Track() == nil {
			continue
		}
		trackID := sender.Track().ID()
		existingSenders[trackID] = true

		if _, ok := desired[trackID]; !ok {
			if err := peer.PC.RemoveTrack(sender); err == nil {
				needRenegotiate = true
				if vt := peer.takeViewerTrack(trackID); vt != nil {
					vt.group.removeViewer(vt)
				}
			}
		}
	}

	for trackID, info := range desired {
		if existingSenders[trackID] {
			continue
		}
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
		sender, err := peer.PC.AddTrack(recipientTrack)
		if err != nil {
			// E.g. the viewer's browser cannot receive the publisher's codec:
			// it then gets no media from this track.
			if !peer.addTrackFailed[trackID] {
				if peer.addTrackFailed == nil {
					peer.addTrackFailed = make(map[string]bool)
				}
				peer.addTrackFailed[trackID] = true
				slog.Warn("sfu add track", "viewer", peer.ID, "publisher", info.SenderID, "source", string(info.Source),
					"codec", info.codecMime(), "err", err)
			}
			continue
		}
		delete(peer.addTrackFailed, trackID)
		needRenegotiate = true
		if vt != nil {
			peer.putViewerTrack(trackID, vt)
			// The new viewer waits for a keyframe of its layer, which
			// addViewer asks for.
			info.group.addViewer(vt)
			go r.forwardRTCP(sender, vt.viewerKeyframe, vt.observeReportWithBandwidth)
			continue
		}
		go r.forwardRTCP(sender, info.requestKeyframe, nil)
		if info.Kind == webrtc.RTPCodecTypeVideo {
			// A new viewer needs a keyframe to start decoding.
			info.requestKeyframe()
		}
	}

	if needRenegotiate || !peer.offerSent {
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
