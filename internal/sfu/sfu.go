package sfu

import (
	"context"
	"fmt"
	"io"
	"log"
	"sync"

	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
)

type Room struct {
	ID        uuid.UUID
	peers     map[uuid.UUID]*Peer
	peersMu   sync.RWMutex
	api       *webrtc.API
	onTrackCB func(senderID uuid.UUID, track *webrtc.TrackRemote)
}

type Peer struct {
	ID         uuid.UUID
	PC         *webrtc.PeerConnection
	AudioTrack *webrtc.TrackLocalStaticRTP
	VideoTrack *webrtc.TrackLocalStaticRTP
	room       *Room
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
		ID:    channelID,
		peers: make(map[uuid.UUID]*Peer),
		api:   s.api,
	}
	s.rooms[channelID] = r
	return r
}

func (r *Room) JoinPeer(ctx context.Context, userID uuid.UUID) (*Peer, error) {
	r.peersMu.Lock()
	defer r.peersMu.Unlock()

	pc, err := r.api.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create PeerConnection: %w", err)
	}

	// Create local outbound audio track for this peer
	audioTrack, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus},
		fmt.Sprintf("audio-%s", userID.String()),
		"mnema-talk-audio",
	)
	if err != nil {
		pc.Close()
		return nil, err
	}

	if _, err := pc.AddTrack(audioTrack); err != nil {
		pc.Close()
		return nil, err
	}

	// Create local outbound video track for 4K 60fps screenshare
	videoTrack, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP9},
		fmt.Sprintf("video-%s", userID.String()),
		"mnema-talk-screenshare",
	)
	if err != nil {
		pc.Close()
		return nil, err
	}

	if _, err := pc.AddTrack(videoTrack); err != nil {
		pc.Close()
		return nil, err
	}

	peer := &Peer{
		ID:         userID,
		PC:         pc,
		AudioTrack: audioTrack,
		VideoTrack: videoTrack,
		room:       r,
	}

	// When this peer sends audio or video, forward RTP packets directly to all other peers in the room
	pc.OnTrack(func(remoteTrack *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		isAudio := remoteTrack.Kind() == webrtc.RTPCodecTypeAudio
		log.Printf("[SFU] Inbound track received: user=%s, kind=%s, codec=%s\n", userID, remoteTrack.Kind(), remoteTrack.Codec().MimeType)

		go func() {
			buf := make([]byte, 1500)
			for {
				n, _, err := remoteTrack.Read(buf)
				if err != nil {
					if err == io.EOF {
						return
					}
					return
				}

				// Forward to other peers in room (Zero-transcoding pure RTP forwarding)
				r.peersMu.RLock()
				for otherID, otherPeer := range r.peers {
					if otherID == userID {
						continue // Do not echo back to sender
					}
					if isAudio && otherPeer.AudioTrack != nil {
						_, _ = otherPeer.AudioTrack.Write(buf[:n])
					} else if !isAudio && otherPeer.VideoTrack != nil {
						_, _ = otherPeer.VideoTrack.Write(buf[:n])
					}
				}
				r.peersMu.RUnlock()
			}
		}()
	})

	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		log.Printf("[SFU] Peer %s connection state: %s\n", userID, state)
		if state == webrtc.PeerConnectionStateFailed || state == webrtc.PeerConnectionStateClosed {
			r.RemovePeer(userID)
		}
	})

	r.peers[userID] = peer
	return peer, nil
}

func (r *Room) RemovePeer(userID uuid.UUID) {
	r.peersMu.Lock()
	defer r.peersMu.Unlock()

	if peer, ok := r.peers[userID]; ok {
		_ = peer.PC.Close()
		delete(r.peers, userID)
		log.Printf("[SFU] Peer %s removed from room %s\n", userID, r.ID)
	}
}
