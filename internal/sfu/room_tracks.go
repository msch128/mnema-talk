package sfu

import (
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/pion/interceptor"
	"github.com/pion/rtcp"
	"github.com/pion/webrtc/v4"
)

// forwardRTCP reads a subscriber's feedback for one forwarded track. Keyframe
// requests go to the publisher; NACKs and reports are handled by interceptors.
func (r *Room) forwardRTCP(sender *webrtc.RTPSender, keyframe func(), report func(loss float64, rtt time.Duration, bandwidth int)) {
	var ssrc uint32
	if report != nil {
		if encodings := sender.GetParameters().Encodings; len(encodings) > 0 {
			ssrc = uint32(encodings[0].SSRC)
		}
	}
	for {
		pkts, attrs, err := sender.ReadRTCP()
		if err != nil {
			return
		}
		for _, p := range pkts {
			handleFeedback(p, attrs, ssrc, keyframe, report)
		}
	}
}

// handleFeedback acts on one RTCP packet from a subscriber: keyframe requests
// go to keyframe, receiver reports for ssrc to report (when set).
func handleFeedback(p rtcp.Packet, attrs interceptor.Attributes, ssrc uint32, keyframe func(), report func(loss float64, rtt time.Duration, bandwidth int)) {
	switch p := p.(type) {
	case *rtcp.PictureLossIndication, *rtcp.FullIntraRequest:
		keyframe()
	case *rtcp.ReceiverReport:
		if report == nil {
			return
		}
		loss, rtt, ok := receiverReportOf(p, ssrc, layerNow())
		if !ok {
			return
		}
		bandwidth := 0
		if src, ok := attrs.Get(bweKey{}).(bandwidthSource); ok {
			bandwidth = src.bandwidth()
		}
		report(loss, rtt, bandwidth)
	}
}

// requestKeyframe asks the publisher for a fresh keyframe of this track.
func (t *TrackInfo) requestKeyframe() {
	now := time.Now().UnixNano()
	last := t.lastPLI.Load()
	if now-last < int64(keyframeMinInterval) || !t.lastPLI.CompareAndSwap(last, now) {
		return
	}
	_ = t.publisher.PC.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: t.ssrc}})
}

func (r *Room) keyframeTargetsLocked(viewerID uuid.UUID) ([]*TrackInfo, []*layerGroup) {
	var tracks []*TrackInfo
	var layered []*layerGroup
	for _, info := range r.trackLocals {
		if info.Kind != webrtc.RTPCodecTypeVideo || !r.wantsLocked(viewerID, info) {
			continue
		}
		if info.group != nil {
			if !slices.Contains(layered, info.group) {
				layered = append(layered, info.group)
			}
			continue
		}
		tracks = append(tracks, info)
	}
	return tracks, layered
}

// DispatchKeyframe asks every publisher whose video viewerID receives for a
// keyframe (the browser sends this when a remote video starts).
func (r *Room) DispatchKeyframe(viewerID uuid.UUID) {
	r.mu.RLock()
	tracks, layered := r.keyframeTargetsLocked(viewerID)
	r.mu.RUnlock()
	for _, info := range tracks {
		info.requestKeyframe()
	}
	// A layered share is asked for only the layer this viewer receives.
	for _, g := range layered {
		if vt := g.viewerFor(viewerID); vt != nil {
			vt.requestKeyframe()
		}
	}
}

// RequestSourceKeyframe asks publisherID for a keyframe of their own video
// of the given source, e.g. when their screen share (re)starts, so everyone
// watching gets a decodable picture right away. A track whose first packets
// have not arrived yet is not published; its viewers get a keyframe when
// they are added to it.
func (r *Room) RequestSourceKeyframe(publisherID uuid.UUID, source Source) {
	r.mu.RLock()
	var tracks []*TrackInfo
	for _, info := range r.trackLocals {
		if info.SenderID == publisherID && info.Source == source && info.Kind == webrtc.RTPCodecTypeVideo {
			tracks = append(tracks, info)
		}
	}
	r.mu.RUnlock()
	for _, info := range tracks {
		info.requestKeyframe()
	}
}

func (r *Room) isTrackStaleLocked(info *TrackInfo) bool {
	return r.peers[info.SenderID] != info.publisher || info.isSuperseded()
}

func (r *Room) supersedePreviousCaptureLocked(info *TrackInfo, sourceKey string) {
	previous := r.latestSources[sourceKey]
	if previous == nil || previous.sameCapture(info) {
		return
	}
	previous.supersede()
	for oldKey, old := range r.trackLocals {
		if old.sameCapture(previous) {
			delete(r.trackLocals, oldKey)
		}
	}
}

func (r *Room) addTrack(key string, info *TrackInfo) {
	r.mu.Lock()
	// The publisher may have been replaced while its track was arriving.
	if r.isTrackStaleLocked(info) {
		r.mu.Unlock()
		return
	}
	if r.latestSources == nil {
		r.latestSources = make(map[string]*TrackInfo)
	}
	sourceKey := trackKey(info.SenderID, string(info.Source))
	r.supersedePreviousCaptureLocked(info, sourceKey)
	r.latestSources[sourceKey] = info
	r.trackLocals[key] = info
	r.mu.Unlock()
	if info.group != nil {
		info.group.markLive(info)
	}
	go r.SignalPeerConnections()
}

func (r *Room) removeTrack(key string, info *TrackInfo) {
	if info.group != nil {
		// The layer is gone for good, whether or not the share was stopped.
		info.group.detach(info)
	}
	r.mu.Lock()
	if r.trackLocals[key] != info {
		r.mu.Unlock()
		return
	}
	delete(r.trackLocals, key)
	r.mu.Unlock()
	go r.SignalPeerConnections()
}

func (r *Room) removePeerTracksLocked(userID uuid.UUID) {
	for key, info := range r.latestSources {
		if info.SenderID == userID {
			info.supersede()
			delete(r.latestSources, key)
		}
	}
	if g := r.groups[userID]; g != nil {
		g.supersede()
		g.close()
		delete(r.groups, userID)
	}
	if g := r.cameraGroups[userID]; g != nil {
		g.supersede()
		g.close()
		delete(r.cameraGroups, userID)
	}
	delete(r.layerMeta, userID)
	delete(r.cameraLayerMeta, userID)
	for id, tInfo := range r.trackLocals {
		if tInfo.SenderID == userID {
			delete(r.trackLocals, id)
		}
	}
	r.dropScreenSubsLocked(userID)
	for _, s := range r.subs {
		s.dropCameraFit(userID)
	}
}

// dropScreenSubsLocked ends every viewer's opt-in to userID's screen share:
// the opt-in belongs to that one share.
func (r *Room) dropScreenSubsLocked(userID uuid.UUID) {
	for _, s := range r.subs {
		s.dropScreen(userID)
	}
}

// supersede marks a replaced capture; the layers of a screen share go together.
func (t *TrackInfo) supersede() {
	t.superseded.Store(true)
	if t.group != nil {
		t.group.supersede()
	}
}

// RemoveUserSource unpublishes the user's screen share (with its sound) or
// camera (the client announces that it stopped sending it) and signals the
// other peers. The track is published again when its packets resume.
func (r *Room) RemoveUserSource(userID uuid.UUID, source Source) {
	if source != SourceScreen && source != SourceCamera {
		return
	}
	r.unpublish(userID, source)
}

// RevokeUserSource unpublishes a source because the user may no longer send
// it (their Speak or Video permission was taken away). Unlike the client's
// own stop it also covers the voice.
func (r *Room) RevokeUserSource(userID uuid.UUID, source Source) {
	if source != SourceScreen && source != SourceCamera && source != SourceAudio {
		return
	}
	r.unpublish(userID, source)
}

func matchesSource(tInfoSource Source, source Source) bool {
	if tInfoSource == source {
		return true
	}
	return source == SourceScreen && tInfoSource == SourceScreenAudio
}

func (r *Room) stopSourceTracksLocked(userID uuid.UUID, source Source) bool {
	changed := false
	for id, tInfo := range r.trackLocals {
		if tInfo.SenderID == userID && matchesSource(tInfo.Source, source) {
			tInfo.stoppedAt.Store(time.Now().UnixNano())
			delete(r.trackLocals, id)
			changed = true
		}
	}
	return changed
}

func (r *Room) stopScreenShareLocked(userID uuid.UUID) {
	r.dropScreenSubsLocked(userID)
	delete(r.layerMeta, userID)
	if g := r.groups[userID]; g != nil {
		g.shareStopped()
	}
}

func (r *Room) unpublish(userID uuid.UUID, source Source) {
	r.mu.Lock()
	changed := r.stopSourceTracksLocked(userID, source)
	if source == SourceScreen {
		r.stopScreenShareLocked(userID)
	}
	if source == SourceCamera {
		r.cameraStoppedLocked(userID)
	}
	r.mu.Unlock()
	if changed {
		go r.SignalPeerConnections()
	}
}

// removePeer closes peer and, if it is still the user's current connection,
// removes it and its tracks from the room. It reports whether peer was the
// current connection.
func (r *Room) removePeer(peer *Peer) bool {
	current := r.detachPeer(peer)
	if current {
		go r.SignalPeerConnections()
	}
	_ = peer.PC.Close()
	return current
}

// dropLostPeer removes a peer whose media connection failed or that stopped
// answering, and tells its owner (see Peer.OnLost), who is otherwise left in
// the call without media.
func (r *Room) dropLostPeer(peer *Peer) {
	if r.removePeer(peer) {
		peer.notifyLost()
	}
}

// detachPeer changes membership without closing network transports. This
// lets the SFU atomically remove an empty room before releasing roomsMu.
func (r *Room) detachPeer(peer *Peer) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	current := r.peers[peer.ID] == peer
	if current {
		delete(r.peers, peer.ID)
		r.load.add(-1)
		delete(r.subs, peer.ID)
		r.removePeerTracksLocked(peer.ID)
	}
	peer.releaseViewerTracks()
	return current
}

// selectedPair is the ICE candidate pair media flows over, for the logs.
func selectedPair(pc *webrtc.PeerConnection) (*webrtc.ICECandidatePair, error) {
	for _, t := range pc.GetTransceivers() {
		if r := t.Receiver(); r != nil && r.Transport() != nil {
			return r.Transport().ICETransport().GetSelectedCandidatePair()
		}
	}
	return nil, nil
}

// Healthy reports whether the peer still has a live connection, one that has
// not failed, closed or lost its transport. A rejoin of the same room over a
// healthy peer changes nothing.
func (p *Peer) Healthy() bool {
	if p == nil || p.PC == nil {
		return false
	}
	switch p.PC.ConnectionState() {
	case webrtc.PeerConnectionStateFailed, webrtc.PeerConnectionStateClosed, webrtc.PeerConnectionStateDisconnected:
		return false
	}
	return true
}

// OnLost registers fn, called once, on its own goroutine, when the SFU gives
// up on the peer's media connection (it failed, or the client never answered
// an offer) and has removed the peer from its room. The owner is still seated
// elsewhere and must be told. fn runs at once if that already happened.
func (p *Peer) OnLost(fn func()) {
	p.lostMu.Lock()
	lost := p.lost
	if !lost {
		p.onLost = fn
	}
	p.lostMu.Unlock()
	if lost && fn != nil {
		go fn()
	}
}

func (p *Peer) notifyLost() {
	p.lostMu.Lock()
	p.lost = true
	fn := p.onLost
	p.onLost = nil
	p.lostMu.Unlock()
	if fn != nil {
		go fn()
	}
}
