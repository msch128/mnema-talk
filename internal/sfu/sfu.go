package sfu

import (
	"fmt"
	"log"
	"sync"

	"github.com/google/uuid"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

type TrackInfo struct {
	Track    *webrtc.TrackLocalStaticRTP
	SenderID uuid.UUID
	Kind     webrtc.RTPCodecType
}

type Peer struct {
	ID        uuid.UUID
	PC        *webrtc.PeerConnection
	SendOffer func(offer webrtc.SessionDescription)
	SendICE   func(candidate *webrtc.ICECandidateInit)
	room      *Room
}

type Room struct {
	ID          uuid.UUID
	peers       map[uuid.UUID]*Peer
	trackLocals map[string]*TrackInfo
	mu          sync.RWMutex
	api         *webrtc.API
}

type SFU struct {
	api     *webrtc.API
	rooms   map[uuid.UUID]*Room
	roomsMu sync.RWMutex
}

func NewSFU(portMin, portMax uint16, nat1to1IP string) (*SFU, error) {
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

	api := webrtc.NewAPI(
		webrtc.WithSettingEngine(settingEngine),
		webrtc.WithMediaEngine(mediaEngine),
	)

	return &SFU{
		api:   api,
		rooms: make(map[uuid.UUID]*Room),
	}, nil
}

func (s *SFU) GetOrCreateRoom(channelID uuid.UUID) *Room {
	s.roomsMu.Lock()
	defer s.roomsMu.Unlock()

	if r, ok := s.rooms[channelID]; ok {
		return r
	}

	r := &Room{
		ID:          channelID,
		peers:       make(map[uuid.UUID]*Peer),
		trackLocals: make(map[string]*TrackInfo),
		api:         s.api,
	}
	s.rooms[channelID] = r
	return r
}

func (r *Room) JoinPeer(userID uuid.UUID, sendOffer func(webrtc.SessionDescription), sendICE func(*webrtc.ICECandidateInit)) (*Peer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if existing, ok := r.peers[userID]; ok {
		_ = existing.PC.Close()
		r.removePeerTracksLocked(userID)
		delete(r.peers, userID)
	}

	pc, err := r.api.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
		},
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

	pc.OnTrack(func(remoteTrack *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		log.Printf("[SFU] Inbound track received: user=%s kind=%s id=%s codec=%s",
			userID, remoteTrack.Kind(), remoteTrack.ID(), remoteTrack.Codec().MimeType)

		trackLocal, err := webrtc.NewTrackLocalStaticRTP(remoteTrack.Codec().RTPCodecCapability, remoteTrack.ID(), remoteTrack.StreamID())
		if err != nil {
			log.Printf("[SFU] Failed to create TrackLocalStaticRTP: %v", err)
			return
		}

		r.addTrack(remoteTrack.ID(), trackLocal, userID, remoteTrack.Kind())

		go func() {
			buf := make([]byte, 1500)
			rtpPkt := &rtp.Packet{}
			for {
				n, _, err := remoteTrack.Read(buf)
				if err != nil {
					r.removeTrack(remoteTrack.ID())
					return
				}
				if err := rtpPkt.Unmarshal(buf[:n]); err != nil {
					continue
				}
				rtpPkt.Extension = false
				rtpPkt.Extensions = nil
				if err := trackLocal.WriteRTP(rtpPkt); err != nil {
					r.removeTrack(remoteTrack.ID())
					return
				}
			}
		}()
	})

	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		log.Printf("[SFU] Peer %s connection state: %s", userID, state)
		if state == webrtc.PeerConnectionStateFailed || state == webrtc.PeerConnectionStateClosed {
			r.RemovePeer(userID)
		}
	})

	r.peers[userID] = peer

	go r.SignalPeerConnections()

	return peer, nil
}

func (r *Room) SignalPeerConnections() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, peer := range r.peers {
		if peer.PC.ConnectionState() == webrtc.PeerConnectionStateClosed {
			continue
		}

		existingSenders := make(map[string]bool)
		for _, sender := range peer.PC.GetSenders() {
			if sender.Track() == nil {
				continue
			}
			trackID := sender.Track().ID()
			existingSenders[trackID] = true

			if _, ok := r.trackLocals[trackID]; !ok {
				_ = peer.PC.RemoveTrack(sender)
			}
		}

		needRenegotiate := false
		for trackID, tInfo := range r.trackLocals {
			if tInfo.SenderID == peer.ID {
				continue
			}
			if !existingSenders[trackID] {
				if _, err := peer.PC.AddTrack(tInfo.Track); err == nil {
					needRenegotiate = true
				}
			}
		}

		if needRenegotiate || peer.PC.LocalDescription() == nil {
			offer, err := peer.PC.CreateOffer(nil)
			if err != nil {
				log.Printf("[SFU] CreateOffer error: %v", err)
				continue
			}
			if err := peer.PC.SetLocalDescription(offer); err != nil {
				log.Printf("[SFU] SetLocalDescription error: %v", err)
				continue
			}
			if peer.SendOffer != nil {
				peer.SendOffer(offer)
			}
		}
	}
}

func (r *Room) DispatchKeyframe(targetUserID uuid.UUID) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if peer, ok := r.peers[targetUserID]; ok {
		for _, receiver := range peer.PC.GetReceivers() {
			if receiver.Track() != nil && receiver.Track().Kind() == webrtc.RTPCodecTypeVideo {
				_ = peer.PC.WriteRTCP([]rtcp.Packet{
					&rtcp.PictureLossIndication{MediaSSRC: uint32(receiver.Track().SSRC())},
				})
			}
		}
	}
}

func (r *Room) addTrack(id string, track *webrtc.TrackLocalStaticRTP, senderID uuid.UUID, kind webrtc.RTPCodecType) {
	r.mu.Lock()
	r.trackLocals[id] = &TrackInfo{
		Track:    track,
		SenderID: senderID,
		Kind:     kind,
	}
	r.mu.Unlock()
	go r.SignalPeerConnections()
}

func (r *Room) removeTrack(id string) {
	r.mu.Lock()
	delete(r.trackLocals, id)
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

func (r *Room) RemovePeer(userID uuid.UUID) {
	r.mu.Lock()
	if peer, ok := r.peers[userID]; ok {
		_ = peer.PC.Close()
		delete(r.peers, userID)
		r.removePeerTracksLocked(userID)
	}
	r.mu.Unlock()
	go r.SignalPeerConnections()
}

func (r *Room) GetPeer(userID uuid.UUID) *Peer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.peers[userID]
}
