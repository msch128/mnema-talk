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

// Source tells what a forwarded track carries. Audio and screen tracks are
// published under the user's ID as stream ID; a camera under CameraStreamPrefix
// plus the user's ID, so clients can tell them apart and map them to people.
type Source string

const (
	SourceAudio  Source = "audio"
	SourceScreen Source = "screen"
	SourceCamera Source = "camera"
)

// CameraStreamPrefix precedes the publisher's user ID in a camera's stream ID.
const CameraStreamPrefix = "cam:"

// resumeHoldoff keeps late packets of a stopped video from re-publishing it.
const resumeHoldoff = 500 * time.Millisecond

// TrackInfo is one forwarded track: the publisher's remote track and the
// local track every other peer subscribes to.
type TrackInfo struct {
	Track     *webrtc.TrackLocalStaticRTP
	SenderID  uuid.UUID
	Kind      webrtc.RTPCodecType
	Source    Source
	publisher *Peer
	ssrc      uint32
	lastPLI   atomic.Int64 // unix nanos of the last keyframe request
	// stoppedAt (unix nanos, 0 = live) is set when the publisher announced the
	// stop; packets arriving later publish the track again.
	stoppedAt atomic.Int64
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

	// subs is what each viewer receives (see Subscriptions); guarded by mu.
	subs map[uuid.UUID]*Subscriptions
	// lastMedia is the announced screen/camera state per publisher; guarded by mu.
	lastMedia map[uuid.UUID]MediaState
	// lastViewers is the announced non-empty audience of each screen share
	// (see notifyMedia); guarded by mu.
	lastViewers map[uuid.UUID][]uuid.UUID
	notifyMu    sync.Mutex
	notifier    *mediaNotifier
}

type SFU struct {
	apiMu sync.RWMutex
	api   *webrtc.API
	// announced are the addresses offered to browsers (see SetAnnouncedIPs).
	announced []string
	portMin   uint16
	portMax   uint16

	iceServers []webrtc.ICEServer
	rooms      map[uuid.UUID]*Room
	roomsMu    sync.Mutex
	notifier   *mediaNotifier
}

// SetMediaStateHandler registers the callback that announces when a user
// starts or stops publishing a screen share or camera.
func (s *SFU) SetMediaStateHandler(fn func(roomID, userID uuid.UUID, state MediaState)) {
	s.notifier.set(fn)
}

// SetScreenViewersHandler registers the callback that announces who watches
// a screen share: the sorted, distinct users in the room subscribed to
// sharer's live share. It runs only when that set changes, with an empty
// list once a watched share ends or loses its last viewer.
func (s *SFU) SetScreenViewersHandler(fn func(roomID, sharer uuid.UUID, viewers []uuid.UUID)) {
	s.notifier.setViewers(fn)
}

// NewSFU creates the SFU. announceIPs are the addresses browsers are told to
// send media to instead of the container's own (WEBRTC_NAT_1TO1_IP): usually
// the public IP and the LAN IP, so both remote and local members connect.
// stunURLs is optional: with announced IPs the server needs no STUN, and
// leaving it empty avoids contacting third parties.
func NewSFU(portMin, portMax uint16, announceIPs []string, stunURLs []string) (*SFU, error) {
	api, err := buildAPI(portMin, portMax, announceIPs)
	if err != nil {
		return nil, err
	}

	var iceServers []webrtc.ICEServer
	if len(stunURLs) > 0 {
		iceServers = []webrtc.ICEServer{{URLs: stunURLs}}
	}

	return &SFU{
		api:        api,
		announced:  append([]string(nil), announceIPs...),
		portMin:    portMin,
		portMax:    portMax,
		iceServers: iceServers,
		rooms:      make(map[uuid.UUID]*Room),
		notifier:   &mediaNotifier{},
	}, nil
}

// AnnouncedIPs returns the addresses currently offered to browsers.
func (s *SFU) AnnouncedIPs() []string {
	s.apiMu.RLock()
	defer s.apiMu.RUnlock()
	return append([]string(nil), s.announced...)
}

// SetAnnouncedIPs switches to new announced addresses (e.g. after the public
// IP of a home connection changed). Connections made from now on use them;
// running ones keep theirs.
func (s *SFU) SetAnnouncedIPs(ips []string) error {
	api, err := buildAPI(s.portMin, s.portMax, ips)
	if err != nil {
		return err
	}
	s.apiMu.Lock()
	s.api = api
	s.announced = append([]string(nil), ips...)
	s.apiMu.Unlock()
	return nil
}

func (s *SFU) currentAPI() *webrtc.API {
	s.apiMu.RLock()
	defer s.apiMu.RUnlock()
	return s.api
}

func buildAPI(portMin, portMax uint16, announceIPs []string) (*webrtc.API, error) {
	settingEngine := webrtc.SettingEngine{}

	if portMin > 0 && portMax > 0 {
		if err := settingEngine.SetEphemeralUDPPortRange(portMin, portMax); err != nil {
			return nil, fmt.Errorf("failed to set UDP port range: %w", err)
		}
	}

	if len(announceIPs) > 0 {
		// Every announced address becomes its own host candidate.
		settingEngine.SetNAT1To1IPs(announceIPs, webrtc.ICECandidateTypeHost)
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

	return webrtc.NewAPI(
		webrtc.WithSettingEngine(settingEngine),
		webrtc.WithMediaEngine(mediaEngine),
		webrtc.WithInterceptorRegistry(registry),
	), nil
}

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
	s.roomsMu.Lock()
	defer s.roomsMu.Unlock()

	r, ok := s.rooms[channelID]
	if !ok {
		r = &Room{
			ID:          channelID,
			peers:       make(map[uuid.UUID]*Peer),
			trackLocals: make(map[string]*TrackInfo),
			subs:        make(map[uuid.UUID]*Subscriptions),
			lastMedia:   make(map[uuid.UUID]MediaState),
			notifier:    s.notifier,
			api:         s.currentAPI(),
			iceServers:  s.iceServers,
		}
		s.rooms[channelID] = r
	}
	// A long-lived room picks up changed announced addresses for new peers.
	r.mu.Lock()
	r.api = s.currentAPI()
	r.mu.Unlock()
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

	// Inbound transceivers to receive the client's mic, screen share and
	// camera. The two video ones are told apart by their position: the first
	// is the screen, the second the camera.
	recvOnly := webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly}
	_, _ = pc.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio, recvOnly)
	screenTr, _ := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo, recvOnly)
	cameraTr, _ := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo, recvOnly)
	pinCodecOrder(screenTr, cameraTr)

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
		source := SourceAudio
		streamID := userID.String()
		if remoteTrack.Kind() == webrtc.RTPCodecTypeVideo {
			source = SourceScreen
			if cameraTr != nil && receiver == cameraTr.Receiver() {
				source = SourceCamera
				streamID = CameraStreamPrefix + streamID
			}
		}
		slog.Info("sfu track received", "user", userID, "kind", remoteTrack.Kind().String(), "source", string(source), "codec", remoteTrack.Codec().MimeType)

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
		case webrtc.ICEConnectionStateFailed, webrtc.ICEConnectionStateDisconnected:
			slog.Warn("sfu ice "+state.String(), "user", userID)
		}
	})

	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		slog.Info("sfu peer state", "user", userID, "state", state.String())
		if state == webrtc.PeerConnectionStateFailed || state == webrtc.PeerConnectionStateClosed {
			r.removePeer(peer)
		}
	})

	r.peers[userID] = peer

	go r.SignalPeerConnections()

	return peer, nil
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
	for {
		n, _, err := remote.Read(buf)
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
		// The publisher stopped and started the same transceiver again:
		// the browser keeps the SSRC, so no new OnTrack fires.
		if at := info.stoppedAt.Load(); at != 0 && time.Since(time.Unix(0, at)) > resumeHoldoff {
			if info.stoppedAt.CompareAndSwap(at, 0) {
				r.addTrack(key, info)
			}
		}
		_ = local.WriteRTP(rtpPkt)
	}
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
	r.notifyMedia()
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

			if info, ok := r.trackLocals[trackID]; !ok || !r.wantsLocked(peer.ID, info) {
				if err := peer.PC.RemoveTrack(sender); err == nil {
					needRenegotiate = true
				}
			}
		}

		for trackID, info := range r.trackLocals {
			if existingSenders[trackID] || !r.wantsLocked(peer.ID, info) {
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
		if info.Kind == webrtc.RTPCodecTypeVideo && r.wantsLocked(viewerID, info) {
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
	r.dropScreenSubsLocked(userID)
}

// dropScreenSubsLocked ends every viewer's opt-in to userID's screen share:
// the opt-in belongs to that one share.
func (r *Room) dropScreenSubsLocked(userID uuid.UUID) {
	for _, s := range r.subs {
		s.dropScreen(userID)
	}
}

// RemoveUserSource unpublishes the user's screen share or camera (the client
// announces that it stopped sending it) and signals the other peers. The
// track is published again when its packets resume.
func (r *Room) RemoveUserSource(userID uuid.UUID, source Source) {
	r.mu.Lock()
	changed := false
	for id, tInfo := range r.trackLocals {
		if tInfo.SenderID == userID && tInfo.Kind == webrtc.RTPCodecTypeVideo && tInfo.Source == source {
			tInfo.stoppedAt.Store(time.Now().UnixNano())
			delete(r.trackLocals, id)
			changed = true
		}
	}
	if source == SourceScreen {
		r.dropScreenSubsLocked(userID)
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
		delete(r.subs, peer.ID)
		r.removePeerTracksLocked(peer.ID)
	}
	r.mu.Unlock()
	_ = peer.PC.Close()
	if current {
		go r.SignalPeerConnections()
	}
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
