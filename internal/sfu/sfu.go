// Package sfu is a selective forwarding unit on Pion WebRTC: every peer sends
// its tracks once and the SFU forwards the RTP to everyone else in the room,
// without transcoding.
package sfu

import (
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/pion/interceptor"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

// keyframeMinInterval rate-limits keyframe requests per forwarded track, so
// a burst of PLIs from several viewers costs the publisher one keyframe.
const keyframeMinInterval = 300 * time.Millisecond

// TrackInfo is one forwarded track: the publisher's remote track and the
// local track every other peer subscribes to.
type TrackInfo struct {
	Track     *webrtc.TrackLocalStaticRTP
	SenderID  uuid.UUID
	Kind      webrtc.RTPCodecType
	publisher *Peer
	ssrc      uint32
	lastPLI   atomic.Int64 // unix nanos of the last keyframe request
}

type Peer struct {
	ID        uuid.UUID
	PC        *webrtc.PeerConnection
	SendOffer func(offer webrtc.SessionDescription)
	SendICE   func(candidate *webrtc.ICECandidateInit)
	room      *Room

	// negotiationPending is set when tracks changed while an offer was still
	// unanswered; the next answer triggers another round.
	negotiationPending atomic.Bool
}

type Room struct {
	ID uuid.UUID
	// peers maps user → their current connection; a rejoin replaces it.
	peers map[uuid.UUID]*Peer
	// trackLocals is keyed by the local track ID, which is scoped to the
	// publishing user (see trackKey) so clients cannot collide.
	trackLocals map[string]*TrackInfo
	mu          sync.RWMutex
	api         *webrtc.API
	iceServers  []webrtc.ICEServer
}

type SFU struct {
	api        *webrtc.API
	iceServers []webrtc.ICEServer
	rooms      map[uuid.UUID]*Room
	roomsMu    sync.Mutex
}

// NewSFU creates the SFU. stunURLs is optional: with WEBRTC_NAT_1TO1_IP set the
// server needs no STUN, and leaving it empty avoids contacting third parties.
func NewSFU(portMin, portMax uint16, nat1to1IP string, stunURLs []string) (*SFU, error) {
	settingEngine := webrtc.SettingEngine{}

	if portMin > 0 && portMax > 0 {
		if err := settingEngine.SetEphemeralUDPPortRange(portMin, portMax); err != nil {
			return nil, fmt.Errorf("failed to set UDP port range: %w", err)
		}
	}

	if nat1to1IP != "" {
		settingEngine.SetNAT1To1IPs([]string{nat1to1IP}, webrtc.ICECandidateTypeHost)
	}

	mediaEngine := &webrtc.MediaEngine{}
	if err := mediaEngine.RegisterDefaultCodecs(); err != nil {
		return nil, fmt.Errorf("failed to register default WebRTC codecs: %w", err)
	}

	// NACK (retransmissions both ways), RTCP sender/receiver reports and
	// transport-wide congestion control feedback to the publishers.
	registry := &interceptor.Registry{}
	if err := webrtc.RegisterDefaultInterceptors(mediaEngine, registry); err != nil {
		return nil, fmt.Errorf("failed to register WebRTC interceptors: %w", err)
	}

	api := webrtc.NewAPI(
		webrtc.WithSettingEngine(settingEngine),
		webrtc.WithMediaEngine(mediaEngine),
		webrtc.WithInterceptorRegistry(registry),
	)

	var iceServers []webrtc.ICEServer
	if len(stunURLs) > 0 {
		iceServers = []webrtc.ICEServer{{URLs: stunURLs}}
	}

	return &SFU{
		api:        api,
		iceServers: iceServers,
		rooms:      make(map[uuid.UUID]*Room),
	}, nil
}

// Room returns the channel's room, or nil if nobody is connected.
func (s *SFU) Room(channelID uuid.UUID) *Room {
	s.roomsMu.Lock()
	defer s.roomsMu.Unlock()
	return s.rooms[channelID]
}

// Join connects userID to the channel's room, replacing an existing
// connection of the same user. Lookup and join happen under one lock, so a
// concurrent RemovePeer cannot drop the room in between.
func (s *SFU) Join(channelID, userID uuid.UUID, sendOffer func(webrtc.SessionDescription), sendICE func(*webrtc.ICECandidateInit)) (*Room, *Peer, error) {
	s.roomsMu.Lock()
	defer s.roomsMu.Unlock()

	r, ok := s.rooms[channelID]
	if !ok {
		r = &Room{
			ID:          channelID,
			peers:       make(map[uuid.UUID]*Peer),
			trackLocals: make(map[string]*TrackInfo),
			api:         s.api,
			iceServers:  s.iceServers,
		}
		s.rooms[channelID] = r
	}
	peer, err := r.JoinPeer(userID, sendOffer, sendICE)
	if err != nil {
		if r.empty() {
			delete(s.rooms, channelID)
		}
		return nil, nil, err
	}
	return r, peer, nil
}

// RemovePeer disconnects peer if it is still the user's current connection
// and drops the room once it is empty. A stale peer (already replaced by a
// rejoin) is only closed, never removing its replacement.
func (s *SFU) RemovePeer(channelID uuid.UUID, peer *Peer) {
	if peer == nil {
		return
	}
	s.roomsMu.Lock()
	defer s.roomsMu.Unlock()
	r, ok := s.rooms[channelID]
	if !ok {
		_ = peer.PC.Close()
		return
	}
	r.removePeer(peer)
	if r.empty() {
		delete(s.rooms, channelID)
	}
}

func (r *Room) empty() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.peers) == 0
}

// trackKey scopes a client-chosen track ID to its publisher.
func trackKey(userID uuid.UUID, trackID string) string {
	return userID.String() + ":" + trackID
}

func (r *Room) JoinPeer(userID uuid.UUID, sendOffer func(webrtc.SessionDescription), sendICE func(*webrtc.ICECandidateInit)) (*Peer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if existing, ok := r.peers[userID]; ok {
		delete(r.peers, userID)
		r.removePeerTracksLocked(userID)
		// Closing fires the old connection's state callback; removePeer
		// ignores it because the user's entry is no longer that peer.
		go func() { _ = existing.PC.Close() }()
	}

	pc, err := r.api.NewPeerConnection(webrtc.Configuration{
		ICEServers: r.iceServers,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create PeerConnection: %w", err)
	}

	// Inbound transceivers to receive client's mic and screen share
	_, _ = pc.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio, webrtc.RTPTransceiverInit{
		Direction: webrtc.RTPTransceiverDirectionRecvonly,
	})
	_, _ = pc.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo, webrtc.RTPTransceiverInit{
		Direction: webrtc.RTPTransceiverDirectionRecvonly,
	})

	peer := &Peer{
		ID:        userID,
		PC:        pc,
		SendOffer: sendOffer,
		SendICE:   sendICE,
		room:      r,
	}

	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c != nil && peer.SendICE != nil {
			cand := c.ToJSON()
			peer.SendICE(&cand)
		}
	})

	pc.OnTrack(func(remoteTrack *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		slog.Info("sfu track received", "user", userID, "kind", remoteTrack.Kind().String(), "codec", remoteTrack.Codec().MimeType)

		key := trackKey(userID, remoteTrack.ID())
		// The stream ID names the publishing user, so clients can map tracks to people.
		trackLocal, err := webrtc.NewTrackLocalStaticRTP(remoteTrack.Codec().RTPCodecCapability, key, userID.String())
		if err != nil {
			slog.Error("sfu create local track", "err", err)
			return
		}

		info := &TrackInfo{
			Track:     trackLocal,
			SenderID:  userID,
			Kind:      remoteTrack.Kind(),
			publisher: peer,
			ssrc:      uint32(remoteTrack.SSRC()),
		}
		r.addTrack(key, info)

		go func() {
			buf := make([]byte, 1500)
			rtpPkt := &rtp.Packet{}
			for {
				n, _, err := remoteTrack.Read(buf)
				if err != nil {
					r.removeTrack(key, info)
					return
				}
				if err := rtpPkt.Unmarshal(buf[:n]); err != nil {
					continue
				}
				// Header extension IDs are negotiated per connection; the
				// subscriber side adds its own (e.g. transport-cc).
				rtpPkt.Extension = false
				rtpPkt.Extensions = nil
				if err := trackLocal.WriteRTP(rtpPkt); err != nil {
					r.removeTrack(key, info)
					return
				}
			}
		}()
	})

	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		slog.Debug("sfu peer state", "user", userID, "state", state.String())
		if state == webrtc.PeerConnectionStateFailed || state == webrtc.PeerConnectionStateClosed {
			r.removePeer(peer)
		}
	})

	r.peers[userID] = peer

	go r.SignalPeerConnections()

	return peer, nil
}

// SetAnswer applies the client's answer and runs a pending renegotiation.
func (p *Peer) SetAnswer(answer webrtc.SessionDescription) error {
	if err := p.PC.SetRemoteDescription(answer); err != nil {
		return err
	}
	if p.negotiationPending.Swap(false) {
		go p.room.SignalPeerConnections()
	}
	return nil
}

func (r *Room) SignalPeerConnections() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, peer := range r.peers {
		if peer.PC.ConnectionState() == webrtc.PeerConnectionStateClosed {
			continue
		}
		// Only one offer may be in flight; the answer re-runs signaling.
		if peer.PC.SignalingState() != webrtc.SignalingStateStable {
			peer.negotiationPending.Store(true)
			continue
		}

		existingSenders := make(map[string]bool)
		needRenegotiate := false
		for _, sender := range peer.PC.GetSenders() {
			if sender.Track() == nil {
				continue
			}
			trackID := sender.Track().ID()
			existingSenders[trackID] = true

			if _, ok := r.trackLocals[trackID]; !ok {
				if err := peer.PC.RemoveTrack(sender); err == nil {
					needRenegotiate = true
				}
			}
		}

		for trackID, info := range r.trackLocals {
			if info.SenderID == peer.ID || existingSenders[trackID] {
				continue
			}
			sender, err := peer.PC.AddTrack(info.Track)
			if err != nil {
				continue
			}
			needRenegotiate = true
			go r.forwardRTCP(sender, info)
			if info.Kind == webrtc.RTPCodecTypeVideo {
				// A new viewer needs a keyframe to start decoding.
				info.requestKeyframe()
			}
		}

		if needRenegotiate || peer.PC.LocalDescription() == nil {
			offer, err := peer.PC.CreateOffer(nil)
			if err != nil {
				slog.Error("sfu create offer", "err", err)
				continue
			}
			if err := peer.PC.SetLocalDescription(offer); err != nil {
				slog.Error("sfu set local description", "err", err)
				continue
			}
			if peer.SendOffer != nil {
				peer.SendOffer(offer)
			}
		}
	}
}

// forwardRTCP reads a subscriber's feedback for one forwarded track. Keyframe
// requests go to the publisher; NACKs and reports are handled by interceptors.
func (r *Room) forwardRTCP(sender *webrtc.RTPSender, info *TrackInfo) {
	for {
		pkts, _, err := sender.ReadRTCP()
		if err != nil {
			return
		}
		for _, p := range pkts {
			switch p.(type) {
			case *rtcp.PictureLossIndication, *rtcp.FullIntraRequest:
				info.requestKeyframe()
			}
		}
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

// DispatchKeyframe asks every publisher whose video viewerID receives for a
// keyframe (the browser sends this when a remote video starts).
func (r *Room) DispatchKeyframe(viewerID uuid.UUID) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, info := range r.trackLocals {
		if info.Kind == webrtc.RTPCodecTypeVideo && info.SenderID != viewerID {
			info.requestKeyframe()
		}
	}
}

func (r *Room) addTrack(key string, info *TrackInfo) {
	r.mu.Lock()
	// The publisher may have been replaced while its track was arriving.
	if r.peers[info.SenderID] != info.publisher {
		r.mu.Unlock()
		return
	}
	r.trackLocals[key] = info
	r.mu.Unlock()
	go r.SignalPeerConnections()
}

func (r *Room) removeTrack(key string, info *TrackInfo) {
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
	for id, tInfo := range r.trackLocals {
		if tInfo.SenderID == userID {
			delete(r.trackLocals, id)
		}
	}
}

// RemoveUserVideoTrack removes video tracks published by userID and signals other peers.
func (r *Room) RemoveUserVideoTrack(userID uuid.UUID) {
	r.mu.Lock()
	changed := false
	for id, tInfo := range r.trackLocals {
		if tInfo.SenderID == userID && tInfo.Kind == webrtc.RTPCodecTypeVideo {
			delete(r.trackLocals, id)
			changed = true
		}
	}
	r.mu.Unlock()
	if changed {
		go r.SignalPeerConnections()
	}
}

// removePeer closes peer and, if it is still the user's current connection,
// removes it and its tracks from the room.
func (r *Room) removePeer(peer *Peer) {
	r.mu.Lock()
	current := r.peers[peer.ID] == peer
	if current {
		delete(r.peers, peer.ID)
		r.removePeerTracksLocked(peer.ID)
	}
	r.mu.Unlock()
	_ = peer.PC.Close()
	if current {
		go r.SignalPeerConnections()
	}
}

func (r *Room) GetPeer(userID uuid.UUID) *Peer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.peers[userID]
}
