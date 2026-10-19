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
func (r *Room) attachPeer(userID uuid.UUID, conn *receiveConn, sendOffer func(webrtc.SessionDescription), sendICE func(*webrtc.ICECandidateInit), authorize func() bool) *Peer {
	r.mu.Lock()
	defer r.mu.Unlock()
	pc, cameraTr, screenAudioTr, lowerTrs := conn.pc, conn.cameraTr, conn.screenAudioTr, conn.lowerTrs

	if existing, ok := r.peers[userID]; ok {
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

	peer := &Peer{
		authorizeMedia: authorize,
		ID:             userID,
		PC:             pc,
		SendOffer:      sendOffer,
		SendICE:        sendICE,
		room:           r,

		allowLoopbackCands: r.allowLoopbackCands,
	}

	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c != nil {
			peer.emitCandidate(c.ToJSON())
		}
	})

	pc.OnTrack(func(remoteTrack *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		source := SourceAudio
		streamID := userID.String()
		layer := 0
		if remoteTrack.Kind() == webrtc.RTPCodecTypeVideo {
			source = SourceScreen
			if cameraTr != nil && receiver == cameraTr.Receiver() {
				source = SourceCamera
				streamID = CameraStreamPrefix + streamID
			}
			for i, tr := range lowerTrs {
				if tr != nil && receiver == tr.Receiver() {
					layer = i + 1
				}
			}
		} else if screenAudioTr != nil && receiver == screenAudioTr.Receiver() {
			source = SourceScreenAudio
			streamID = ScreenAudioStreamPrefix + streamID
		}
		slog.Info("sfu track received", "user", userID, "kind", remoteTrack.Kind().String(), "source", string(source), "layer", layer, "codec", remoteTrack.Codec().MimeType)

		// The user may not send this (no Speak or Video permission): never
		// publish it. Should the permission come back later, the client
		// renegotiates and the track is accepted then.
		if peer.sourceDenied(source) {
			drain(remoteTrack)
			return
		}

		if source == SourceScreen {
			r.receiveScreenLayer(peer, remoteTrack, remoteTrack.ID(), uint32(remoteTrack.SSRC()), remoteTrack.Codec().RTPCodecCapability, layer)
			return
		}

		key := trackKey(userID, remoteTrack.ID())
		// The stream ID names the publishing user, so clients can map tracks to people.
		trackLocal, err := webrtc.NewTrackLocalStaticRTP(remoteTrack.Codec().RTPCodecCapability, key, streamID)
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
	})

	pc.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		switch state {
		case webrtc.ICEConnectionStateConnected:
			attrs := []any{"user", userID}
			if pair, err := selectedPair(pc); err == nil && pair != nil {
				attrs = append(attrs, "local", pair.Local.Address, "remote", pair.Remote.Address, "remote_type", pair.Remote.Typ.String())
			}
			slog.Info("sfu ice connected", attrs...)
			metrics.SFUICE.With("connected").Inc()
		case webrtc.ICEConnectionStateFailed, webrtc.ICEConnectionStateDisconnected:
			slog.Warn("sfu ice "+state.String(), "user", userID)
			metrics.SFUICE.With(state.String()).Inc()
		}
	})

	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		slog.Info("sfu peer state", "user", userID, "state", state.String())
		if state == webrtc.PeerConnectionStateFailed || state == webrtc.PeerConnectionStateClosed {
			r.removePeer(peer)
		}
	})

	r.peers[userID] = peer
	r.load.add(1)
	metrics.SFUPeerJoins.Inc()

	go r.SignalPeerConnections()

	return peer
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
		if verdict := limit.admit(n, time.Now()); verdict != ingressOK {
			metrics.SFUIngressDropped.With("rate").Inc()
			if verdict == ingressAbusive {
				ingressAbuse(info)
			}
			continue
		}
		if err := rtpPkt.Unmarshal(buf[:n]); err != nil {
			continue
		}
		metrics.SFUIngressPackets.Inc()
		metrics.SFUIngressBytes.Add(uint64(n))
		if info.isSuperseded() || info.publisher != nil && !info.publisher.mediaAuthorized() {
			return
		}
		// Permission revoked while the track was live: drop its packets.
		if info.publisher != nil && info.publisher.sourceDenied(info.Source) {
			continue
		}
		// Header extension IDs are negotiated per connection; the
		// subscriber side adds its own (e.g. transport-cc).
		rtpPkt.Extension = false
		rtpPkt.Extensions = nil
		// The publisher stopped and started the same transceiver again:
		// the browser keeps the SSRC, so no new OnTrack fires.
		if at := info.stoppedAt.Load(); at != 0 && time.Since(time.Unix(0, at)) > resumeHoldoff {
			if info.stoppedAt.CompareAndSwap(at, 0) {
				r.addTrack(key, info)
			}
		}
		// io.ErrClosedPipe only means a subscriber's sender just stopped.
		if err := local.WriteRTP(rtpPkt); err != nil && !errors.Is(err, io.ErrClosedPipe) && info.writeErrLogged.CompareAndSwap(false, true) {
			slog.Warn("sfu forward rtp", "publisher", info.SenderID, "source", string(info.Source), "err", err)
		}
	}
}
