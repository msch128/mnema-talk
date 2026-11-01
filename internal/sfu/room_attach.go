package sfu

import (
	"errors"
	"io"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/metrics"
	"github.com/pion/interceptor"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

// attachPeer makes conn the user's connection in the room, replacing an
// earlier one. It only wires callbacks and maps: nothing in it waits.
func (r *Room) attachPeer(userID uuid.UUID, conn *receiveConn, sendOffer func(webrtc.SessionDescription), sendICE func(*webrtc.ICECandidateInit)) *Peer {
	r.mu.Lock()
	defer r.mu.Unlock()
	pc := conn.pc

	r.replacePeerLocked(userID)

	peer := &Peer{
		ID:        userID,
		PC:        pc,
		SendOffer: sendOffer,
		SendICE:   sendICE,
		room:      r,

		allowLoopbackCands: r.allowLoopbackCands,
	}

	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c != nil {
			peer.emitCandidate(c.ToJSON())
		}
	})

	pc.OnTrack(func(remoteTrack *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		r.handleRemoteTrack(peer, conn, remoteTrack, receiver)
	})

	pc.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		logICEState(pc, userID, state, r.iceSeen)
	})

	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		slog.Info("sfu peer state", "user", userID, "state", state.String())
		switch state {
		case webrtc.PeerConnectionStateFailed:
			r.dropLostPeer(peer)
		case webrtc.PeerConnectionStateClosed:
			r.removePeer(peer)
		}
	})

	r.peers[userID] = peer
	r.load.add(1)
	metrics.SFUPeerJoins.Inc()

	go r.SignalPeerConnections()

	return peer
}

// replacePeerLocked drops the user's earlier connection, if any. r.mu is held.
func (r *Room) replacePeerLocked(userID uuid.UUID) {
	existing, ok := r.peers[userID]
	if !ok {
		return
	}
	delete(r.peers, userID)
	r.load.add(-1)
	r.removePeerTracksLocked(userID)
	// Closing fires the old connection's state callback; removePeer
	// ignores it because the user's entry is no longer that peer.
	go func() {
		existing.releaseViewerTracks()
		_ = existing.PC.Close()
	}()
}

// logICEState records ICE transitions worth seeing in the log and metrics.
func logICEState(pc *webrtc.PeerConnection, userID uuid.UUID, state webrtc.ICEConnectionState, seen *iceObserver) {
	switch state {
	case webrtc.ICEConnectionStateConnected:
		attrs := []any{"user", userID}
		pair, err := selectedPair(pc)
		if err == nil && pair != nil {
			attrs = append(attrs, "local", pair.Local.Address, "remote", pair.Remote.Address, "remote_type", pair.Remote.Typ.String())
			seen.connected(pair)
		}
		slog.Info("sfu ice connected", attrs...)
		metrics.SFUICE.With("connected").Inc()
	case webrtc.ICEConnectionStateFailed, webrtc.ICEConnectionStateDisconnected:
		slog.Warn("sfu ice "+state.String(), "user", userID)
		metrics.SFUICE.With(state.String()).Inc()
	}
}

// remoteTrackRole says what a received track carries: its source, the stream
// ID viewers see, and, for screen video, the simulcast layer.
type remoteTrackRole struct {
	source   Source
	streamID string
	layer    int
}

// classifyRemoteTrack maps the transceiver a track arrived on to its role.
func classifyRemoteTrack(userID uuid.UUID, conn *receiveConn, remoteTrack *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) remoteTrackRole {
	if remoteTrack.Kind() != webrtc.RTPCodecTypeVideo {
		return classifyAudioTrack(userID, conn, receiver)
	}
	return classifyVideoTrack(userID, conn, receiver)
}

func classifyAudioTrack(userID uuid.UUID, conn *receiveConn, receiver *webrtc.RTPReceiver) remoteTrackRole {
	role := remoteTrackRole{source: SourceAudio, streamID: userID.String()}
	if conn.screenAudioTr != nil && receiver == conn.screenAudioTr.Receiver() {
		role.source = SourceScreenAudio
		role.streamID = ScreenAudioStreamPrefix + role.streamID
	}
	return role
}

func classifyVideoTrack(userID uuid.UUID, conn *receiveConn, receiver *webrtc.RTPReceiver) remoteTrackRole {
	role := remoteTrackRole{source: SourceScreen, streamID: userID.String()}
	if conn.cameraTr != nil && receiver == conn.cameraTr.Receiver() {
		role.source = SourceCamera
		role.streamID = CameraStreamPrefix + role.streamID
	}
	if layer := matchSimulcastLayer(conn.cameraLowerTrs, receiver); layer > 0 {
		// A lower layer of the camera.
		role.source = SourceCamera
		role.streamID = CameraStreamPrefix + userID.String()
		role.layer = layer
		return role
	}
	role.layer = matchSimulcastLayer(conn.lowerTrs, receiver)
	return role
}

func matchSimulcastLayer(trs []*webrtc.RTPTransceiver, receiver *webrtc.RTPReceiver) int {
	for i, tr := range trs {
		if tr != nil && receiver == tr.Receiver() {
			return i + 1
		}
	}
	return 0
}

// handleRemoteTrack publishes a track the peer sends, unless the peer may not
// send that source.
func (r *Room) handleRemoteTrack(peer *Peer, conn *receiveConn, remoteTrack *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
	userID := peer.ID
	role := classifyRemoteTrack(userID, conn, remoteTrack, receiver)
	source := role.source
	slog.Info("sfu track received", "user", userID, "kind", remoteTrack.Kind().String(), "source", string(source), "layer", role.layer, "codec", remoteTrack.Codec().MimeType)

	// The user may not send this (no Speak or Video permission): never
	// publish it while that lasts. Pion reports a track once, so it is kept
	// and published when the first packet arrives after the permission came
	// back.
	if peer.sourceDenied(source) {
		go func() {
			if awaitSourceAllowed(peer, source, remoteTrack) {
				r.publishRemoteTrack(peer, remoteTrack, role)
			}
		}()
		return
	}
	r.publishRemoteTrack(peer, remoteTrack, role)
}

// awaitSourceAllowed reads and drops the packets of a track whose source the
// peer may not send, until the peer may. It returns false when the track ended
// first.
func awaitSourceAllowed(peer *Peer, source Source, remote rtpReader) bool {
	buf := make([]byte, 1500)
	for peer.sourceDenied(source) {
		if _, _, err := remote.Read(buf); err != nil {
			return false
		}
	}
	return true
}

// publishRemoteTrack makes a track the peer may send available to the room
// and starts forwarding its packets.
func (r *Room) publishRemoteTrack(peer *Peer, remoteTrack *webrtc.TrackRemote, role remoteTrackRole) {
	userID := peer.ID
	source := role.source
	if source == SourceScreen {
		r.receiveScreenLayer(peer, remoteTrack, remoteTrack.ID(), uint32(remoteTrack.SSRC()), remoteTrack.Codec().RTPCodecCapability, role.layer)
		return
	}
	if source == SourceCamera && r.takesCameraLayer(userID) {
		r.receiveLayer(peer, remoteTrack, SourceCamera, remoteTrack.ID(), uint32(remoteTrack.SSRC()), remoteTrack.Codec().RTPCodecCapability, role.layer)
		return
	}
	if role.layer > 0 {
		// A lower layer of a camera that is not sent as layers: nothing to join.
		drain(remoteTrack)
		return
	}

	key := trackKey(userID, remoteTrack.ID())
	// The stream ID names the publishing user, so clients can map tracks to people.
	trackLocal, err := webrtc.NewTrackLocalStaticRTP(remoteTrack.Codec().RTPCodecCapability, key, role.streamID)
	if err != nil {
		slog.Error("sfu create local track", "err", err)
		return
	}

	info := &TrackInfo{
		Track:     trackLocal,
		SenderID:  userID,
		Kind:      remoteTrack.Kind(),
		Source:    source,
		publisher: peer,
		ssrc:      uint32(remoteTrack.SSRC()),
	}
	r.addTrack(key, info)

	go r.forward(remoteTrack, trackLocal, key, info)
}

// takesCameraLayer reports whether a camera video goes into a layer group:
// when its sender declared layers before its video started, or when the
// camera already has a group (a restart keeps it). Any layer of it may arrive
// first.
func (r *Room) takesCameraLayer(userID uuid.UUID) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.cameraLayeredLocked(userID) {
		return true
	}
	g := r.cameraGroups[userID]
	return g != nil && !g.superseded.Load()
}

// pinCodecOrder keeps the codec order of the first offer on a publisher's
// video lines for the whole connection. Without it Pion re-offers every video
// line in the order of the client's first answered video line, and the web
// client puts H.264 first on its screen line. A browser that can send H.264
// then switches its running camera from VP8 to H.264 at the next
// renegotiation (e.g. someone joins), while the forwarded track keeps the
// codec of the camera's first packets: viewers get H.264 labelled as VP8 and
// never decode a frame. The client still reorders the screen line itself.
func pinCodecOrder(transceivers ...*webrtc.RTPTransceiver) {
	for _, tr := range transceivers {
		if tr == nil {
			continue
		}
		// Before negotiation these are the registered codecs in their order.
		if err := tr.SetCodecPreferences(tr.Receiver().GetParameters().Codecs); err != nil {
			slog.Warn("sfu pin codec order", "err", err)
		}
	}
}

// rtpReader is the publisher side of a forwarded track (*webrtc.TrackRemote).
type rtpReader interface {
	Read(b []byte) (int, interceptor.Attributes, error)
}

// rtpWriter is the subscriber side (*webrtc.TrackLocalStaticRTP).
type rtpWriter interface {
	WriteRTP(p *rtp.Packet) error
}

// forward copies a publisher's packets to the local track every subscriber
// is bound to, until the publisher's track ends. A write error only concerns
// single bindings (a subscriber whose sender just stopped reports
// io.ErrClosedPipe); the others still got the packet, so it never ends the
// track for the whole room.
func (r *Room) forward(remote rtpReader, local rtpWriter, key string, info *TrackInfo) {
	buf := make([]byte, 1500)
	rtpPkt := &rtp.Packet{}
	limit := newIngressBucket(info.Kind)
	for {
		n, _, err := remote.Read(buf)
		if err != nil {
			r.removeTrack(key, info)
			return
		}
		if !admitIngress(limit, n, info) || !parseForwardPacket(buf, n, rtpPkt) {
			continue
		}
		switch publisherVerdict(info) {
		case endTrack:
			return
		case dropPacket:
			continue
		}
		r.writeForwardedPacket(local, rtpPkt, key, info)
	}
}

func parseForwardPacket(buf []byte, n int, rtpPkt *rtp.Packet) bool {
	if err := rtpPkt.Unmarshal(buf[:n]); err != nil {
		return false
	}
	metrics.SFUIngressPackets.Inc()
	metrics.SFUIngressBytes.Add(uint64(n))
	return true
}

func (r *Room) writeForwardedPacket(local rtpWriter, rtpPkt *rtp.Packet, key string, info *TrackInfo) {
	// Header extension IDs are negotiated per connection; the
	// subscriber side adds its own (e.g. transport-cc).
	rtpPkt.Extension = false
	rtpPkt.Extensions = nil
	r.resumeStoppedTrack(key, info)
	if err := local.WriteRTP(rtpPkt); isLoggedWriteError(err, info) {
		slog.Warn("sfu forward rtp", "publisher", info.SenderID, "source", string(info.Source), "err", err)
	}
}

func isLoggedWriteError(err error, info *TrackInfo) bool {
	if err == nil || errors.Is(err, io.ErrClosedPipe) {
		return false
	}
	return info.writeErrLogged.CompareAndSwap(false, true)
}

// admitIngress applies the publisher's rate limit to an n-byte packet and
// reports whether it may be forwarded.
func admitIngress(limit *ingressBucket, n int, info *TrackInfo) bool {
	verdict := limit.admit(n, time.Now())
	if verdict == ingressOK {
		return true
	}
	metrics.SFUIngressDropped.With("rate").Inc()
	if verdict == ingressAbusive {
		ingressAbuse(info)
	}
	return false
}

// packetVerdict is what to do with a packet after the publisher checks.
type packetVerdict int

const (
	forwardPacket packetVerdict = iota
	dropPacket
	endTrack
)

// publisherVerdict re-checks, per packet, that the track is still current and
// its publisher still allowed to send it.
func publisherVerdict(info *TrackInfo) packetVerdict {
	if info.isSuperseded() {
		return endTrack
	}
	// Permission revoked while the track was live: drop its packets.
	if info.publisher != nil && info.publisher.sourceDenied(info.Source) {
		return dropPacket
	}
	return forwardPacket
}

// resumeStoppedTrack re-adds a track whose publisher stopped and started the
// same transceiver again: the browser keeps the SSRC, so no new OnTrack fires.
func (r *Room) resumeStoppedTrack(key string, info *TrackInfo) {
	at := info.stoppedAt.Load()
	if at == 0 || time.Since(time.Unix(0, at)) <= resumeHoldoff {
		return
	}
	if info.stoppedAt.CompareAndSwap(at, 0) {
		r.addTrack(key, info)
	}
}
