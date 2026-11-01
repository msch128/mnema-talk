package sfu

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
)

// Room returns the channel's room, or nil if nobody is connected.
func (s *SFU) Room(channelID uuid.UUID) *Room {
	s.roomsMu.Lock()
	defer s.roomsMu.Unlock()
	return s.rooms[channelID]
}

// AllMediaStates returns the active media states across all rooms: roomID -> (userID -> MediaState).
func (s *SFU) AllMediaStates() map[uuid.UUID]map[uuid.UUID]MediaState {
	s.roomsMu.Lock()
	defer s.roomsMu.Unlock()
	out := make(map[uuid.UUID]map[uuid.UUID]MediaState, len(s.rooms))
	for id, r := range s.rooms {
		st := r.MediaStates()
		if len(st) > 0 {
			out[id] = st
		}
	}
	return out
}

// Join connects userID to the channel's room, replacing an existing
// connection of the same user. Lookup and join happen under one lock, so a
// concurrent RemovePeer cannot drop the room in between.
func (s *SFU) Join(channelID, userID uuid.UUID, sendOffer func(webrtc.SessionDescription), sendICE func(*webrtc.ICECandidateInit)) (*Room, *Peer, error) {
	if s.closed.Load() {
		return nil, nil, errSFUClosed
	}
	// The connection is built before any lock is taken: creating it costs
	// real time (transports, DTLS and ICE agents), and roomsMu serializes
	// every join, leave and lookup of every room.
	api, announced := s.apiAndAnnounced()
	conn, err := newReceiveConn(api, s.iceServers)
	if err != nil {
		return nil, nil, err
	}
	if s.afterBuild != nil {
		s.afterBuild() // tests close the SFU exactly here
	}

	s.roomsMu.Lock()
	defer s.roomsMu.Unlock()
	if s.closed.Load() {
		_ = conn.pc.Close()
		return nil, nil, errSFUClosed
	}
	r, ok := s.rooms[channelID]
	if !ok {
		r = &Room{
			ID:           channelID,
			load:         s.load,
			peers:        make(map[uuid.UUID]*Peer),
			trackLocals:  make(map[string]*TrackInfo),
			subs:         make(map[uuid.UUID]*Subscriptions),
			lastMedia:    make(map[uuid.UUID]MediaState),
			notifier:     s.notifier,
			iceSeen:      s.iceSeen,
			iceServers:   s.iceServers,
			offerTimeout: s.offerTimeout,
		}
		s.rooms[channelID] = r
	}
	// A long-lived room picks up changed announced addresses for new peers.
	r.mu.Lock()
	r.allowLoopbackCands = announcesLoopback(announced)
	r.mu.Unlock()
	return r, r.attachPeer(userID, conn, sendOffer, sendICE), nil
}

// RemovePeer disconnects peer if it is still the user's current connection
// and drops the room once it is empty. A stale peer (already replaced by a
// rejoin) is only closed, never removing its replacement.
func (s *SFU) RemovePeer(channelID uuid.UUID, peer *Peer) {
	if peer == nil {
		return
	}
	s.roomsMu.Lock()
	r, ok := s.rooms[channelID]
	if !ok {
		s.roomsMu.Unlock()
		_ = peer.PC.Close()
		return
	}
	current := r.detachPeer(peer)
	if r.empty() {
		delete(s.rooms, channelID)
	}
	s.roomsMu.Unlock()
	// Closing transports may wait on network operations. Other rooms and
	// WebSocket registration must remain available while this peer closes.
	if current {
		go r.SignalPeerConnections()
	}
	_ = peer.PC.Close()
}

// CloseRoom disconnects every peer of a room and drops it, e.g. because its
// channel was deleted.
func (s *SFU) CloseRoom(channelID uuid.UUID) {
	s.roomsMu.Lock()
	r, ok := s.rooms[channelID]
	delete(s.rooms, channelID)
	s.roomsMu.Unlock()
	if !ok {
		return
	}
	r.mu.RLock()
	peers := make([]*Peer, 0, len(r.peers))
	for _, p := range r.peers {
		peers = append(peers, p)
	}
	r.mu.RUnlock()
	for _, p := range peers {
		r.removePeer(p)
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

// receiveConn is a PeerConnection with the lines that receive a client's
// media, built before it belongs to a room.
type receiveConn struct {
	pc            *webrtc.PeerConnection
	cameraTr      *webrtc.RTPTransceiver
	screenAudioTr *webrtc.RTPTransceiver
	lowerTrs      []*webrtc.RTPTransceiver
	// cameraLowerTrs are the lines of the camera's lower layers, after those of the screen's.
	cameraLowerTrs []*webrtc.RTPTransceiver
}

func newReceiveConn(api *webrtc.API, iceServers []webrtc.ICEServer) (*receiveConn, error) {
	pc, err := api.NewPeerConnection(webrtc.Configuration{
		ICEServers: iceServers,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create PeerConnection: %w", err)
	}

	// Inbound transceivers to receive the client's mic, screen share, camera
	// and the screen share's sound, in this order. Lines of one kind are told
	// apart by their position: the first video line is the screen, the second
	// the camera; the first audio line is the mic, the second the screen's
	// sound. After them come the lines of the screen's lower layers (see
	// layerGroup) and then those of the camera's: a sender that does not know
	// them answers them inactive.
	recvOnly := webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly}
	if _, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio, recvOnly); err != nil {
		_ = pc.Close()
		return nil, fmt.Errorf("failed to add mic transceiver: %w", err)
	}
	screenTr, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo, recvOnly)
	if err != nil {
		_ = pc.Close()
		return nil, fmt.Errorf("failed to add screen transceiver: %w", err)
	}
	cameraTr, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo, recvOnly)
	if err != nil {
		_ = pc.Close()
		return nil, fmt.Errorf("failed to add camera transceiver: %w", err)
	}
	screenAudioTr, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio, recvOnly)
	if err != nil {
		_ = pc.Close()
		return nil, fmt.Errorf("failed to add screen audio transceiver: %w", err)
	}
	lowerTrs := make([]*webrtc.RTPTransceiver, 0, maxLayers-1)
	for range maxLayers - 1 {
		tr, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo, recvOnly)
		if err != nil {
			_ = pc.Close()
			return nil, fmt.Errorf("failed to add layer transceiver: %w", err)
		}
		lowerTrs = append(lowerTrs, tr)
	}
	cameraLowerTrs := make([]*webrtc.RTPTransceiver, 0, maxLayers-1)
	for range maxLayers - 1 {
		tr, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo, recvOnly)
		if err != nil {
			_ = pc.Close()
			return nil, fmt.Errorf("failed to add camera layer transceiver: %w", err)
		}
		cameraLowerTrs = append(cameraLowerTrs, tr)
	}
	pinCodecOrder(append(append([]*webrtc.RTPTransceiver{screenTr, cameraTr}, lowerTrs...), cameraLowerTrs...)...)

	return &receiveConn{pc: pc, cameraTr: cameraTr, screenAudioTr: screenAudioTr, lowerTrs: lowerTrs, cameraLowerTrs: cameraLowerTrs}, nil
}
