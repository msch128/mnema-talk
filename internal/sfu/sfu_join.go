package sfu

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/safego"
	"github.com/pion/interceptor"
	"github.com/pion/rtp"
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
// concurrent RemovePeer cannot drop the room in between. connID is the id of
// the hub connection that joins (conn_id on the peer's log lines), or "".
func (s *SFU) Join(channelID, userID uuid.UUID, connID string, sendOffer func(webrtc.SessionDescription), sendICE func(*webrtc.ICECandidateInit)) (*Room, *Peer, error) {
	if s.closed.Load() {
		return nil, nil, errSFUClosed
	}
	// The connection is built before any lock is taken: creating it costs
	// real time (transports, DTLS and ICE agents), and roomsMu serializes
	// every join, leave and lookup of every room.
	api, announced := s.apiAndAnnounced()
	conn, err := newReceiveConn(api, s.iceServers, joinLogger(userID, connID))
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
			conns:        &s.conns,
		}
		s.rooms[channelID] = r
	}
	// A long-lived room picks up changed announced addresses for new peers.
	loopback := announcesLoopback(announced)
	r.mu.Lock()
	r.allowLoopbackCands = loopback
	r.mu.Unlock()
	return r, r.attachPeer(userID, connID, conn, sendOffer, sendICE), nil
}

// RemovePeer disconnects peer if it is still the user's current connection
// and drops the room once it is empty. A stale peer (already replaced by a
// rejoin) is only closed, never removing its replacement.
func (s *SFU) RemovePeer(channelID uuid.UUID, peer *Peer) {
	if peer == nil {
		return
	}
	var r *Room
	ok, current := false, false
	safego.Locked(&s.roomsMu, func() {
		if r, ok = s.rooms[channelID]; !ok {
			return
		}
		current = r.detachPeer(peer)
		if r.empty() {
			delete(s.rooms, channelID)
		}
	})
	if !ok {
		_ = peer.PC.Close()
		return
	}
	// Closing transports may wait on network operations. Other rooms and
	// WebSocket registration must remain available while this peer closes.
	if current {
		r.goSignal()
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

func newReceiveConn(api *webrtc.API, iceServers []webrtc.ICEServer, log *slog.Logger) (*receiveConn, error) {
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
	pinCodecOrder(log, append(append([]*webrtc.RTPTransceiver{screenTr, cameraTr}, lowerTrs...), cameraLowerTrs...)...)

	return &receiveConn{pc: pc, cameraTr: cameraTr, screenAudioTr: screenAudioTr, lowerTrs: lowerTrs, cameraLowerTrs: cameraLowerTrs}, nil
}

// What the SFU does when its own code panics (see internal/safego). Work that
// belongs to one peer removes that peer and tells its owner, like a media
// connection that failed; the room keeps forwarding for everyone else. Work
// on a whole room drops every peer of that room, who then join again. Only
// the process-wide loops (address refresh, upload limit, announcements to the
// hub) let the process exit.

// dropOnPanic is the onPanic of work that belongs to peer. The owner is told
// whether or not this call is the one that takes the peer out of the room:
// the panic may have come from a removal that had detached it already and
// never got to tell. Telling is once-only, and the hub ignores it for a peer
// its member has replaced.
func (r *Room) dropOnPanic(peer *Peer) func() {
	return func() {
		r.removePeer(peer)
		peer.notifyLost()
	}
}

// dropViewerPeer removes the peer of userID a panicking viewer track was made
// for (gen), on a goroutine of its own: the caller is another peer's
// forwarding loop. A member who joined again since has a new, healthy peer,
// which stays.
func (r *Room) dropViewerPeer(userID uuid.UUID, gen uint64) {
	r.mu.RLock()
	peer := r.peers[userID]
	r.mu.RUnlock()
	if peer != nil && peer.gen == gen {
		r.goDropLost(peer)
	}
}

// viewerGuardFactory puts a viewerGuard on top of every connection's chain of
// interceptors. It is added to the registry last, which makes its writer the
// first one a packet passes.
type viewerGuardFactory struct {
	// drop removes the peer whose connection has the given ID.
	drop func(connID string)
	// names knows the hub connection behind that ID, for the panic's log line.
	names *connNames
}

func (f viewerGuardFactory) NewInterceptor(connID string) (interceptor.Interceptor, error) {
	return &viewerGuard{connID: connID, drop: f.drop, names: f.names}, nil
}

// viewerGuard keeps a panic in one viewer's connection with that viewer. A
// track that is shared by its viewers (a microphone, the sound of a screen, a
// camera sent plain) writes every packet to each viewer's chain (NACK
// history, counters, transport-cc stamp, bandwidth estimate, SRTP) on the
// publisher's forwarding goroutine. Without the guard a panic there would
// drop the member who speaks, again after every rejoin, while the connection
// that caused it stayed.
type viewerGuard struct {
	interceptor.NoOp
	connID string
	drop   func(connID string)
	names  *connNames
	// dead: a write panicked. Nothing more goes into the chain it left
	// half-updated, while the peer is on its way out.
	dead atomic.Bool
}

// Close ends the connection's entry in names with the connection.
func (g *viewerGuard) Close() error {
	g.names.forget(g.connID)
	return nil
}

func (g *viewerGuard) BindLocalStream(_ *interceptor.StreamInfo, w interceptor.RTPWriter) interceptor.RTPWriter {
	return &guardedWriter{guard: g, next: w}
}

// guardedWriter is the writer of one outgoing stream of a guarded connection.
type guardedWriter struct {
	guard *viewerGuard
	next  interceptor.RTPWriter
}

// Write runs once per packet and viewer: it must not allocate.
func (w *guardedWriter) Write(h *rtp.Header, payload []byte, a interceptor.Attributes) (n int, err error) {
	if w.guard.dead.Load() {
		return 0, io.ErrClosedPipe
	}
	defer w.guard.writeDone(&err)
	return w.next.Write(h, payload, a)
}

// writeDone is deferred by Write. It costs a few nanoseconds per packet and
// allocates nothing unless the write panicked. The writer reports a closed
// stream then, like a viewer whose sender just stopped.
func (g *viewerGuard) writeDone(err *error) {
	rec := recover()
	if rec == nil {
		return
	}
	*err = io.ErrClosedPipe
	g.dead.Store(true)
	// The caller holds locks of the track it writes to: the peer is looked
	// up and removed on a goroutine of its own.
	safego.RecoveredConn(g.names.of(g.connID), "sfu_viewer_write", rec, func() {
		safego.GoConn(g.names.of(g.connID), "sfu_peer_drop", safego.Crash, func() { g.drop(g.connID) })
	})
}

// dropConnection removes the peer whose connection is connID and tells its
// owner. A connection the member has replaced is gone already. Pion names a
// connection after the nanosecond it was made in, and connections are made
// side by side: should two share a name, both go and join again, rather
// than the healthy one alone while the broken one stays unheard.
func (s *SFU) dropConnection(connID string) {
	var found []*Peer
	safego.Locked(&s.roomsMu, func() {
		for _, r := range s.rooms {
			safego.Locked(r.mu.RLocker(), func() {
				for _, peer := range r.peers {
					if peer.connID == connID {
						found = append(found, peer)
					}
				}
			})
		}
	})
	for _, peer := range found {
		peer.room.goDropLost(peer)
	}
}

// goSignal runs a signaling round of the whole room on a goroutine of its own.
func (r *Room) goSignal() {
	safego.Go("sfu_signal", r.dropAllPeers, r.SignalPeerConnections)
}

// goDropLost removes peer on a goroutine of its own. When even that panics
// the connection is closed and the owner told, so the member does not stay
// in the call unheard.
func (r *Room) goDropLost(peer *Peer) {
	giveUp := func() {
		_ = peer.PC.Close()
		peer.notifyLost()
	}
	safego.GoConn(peer.panics(), "sfu_peer_drop", giveUp, func() { r.dropLostPeer(peer) })
}

// goForwardRTCP reads viewer's feedback for one track it receives.
func (r *Room) goForwardRTCP(viewer *Peer, sender *webrtc.RTPSender, keyframe func(), report func(loss float64, rtt time.Duration, bandwidth int)) {
	safego.GoConn(viewer.panics(), "sfu_rtcp", r.dropOnPanic(viewer), func() {
		r.feedbackEnded(viewer, r.forwardRTCP(sender, keyframe, report))
	})
}

// feedbackEnded looks at why reading a viewer's feedback stopped. A
// connection whose NACK responder gave up would go on without
// retransmissions: the viewer is dropped and joins again.
func (r *Room) feedbackEnded(viewer *Peer, err error) {
	if errors.Is(err, errNackGaveUp) {
		viewer.logger().Error("sfu nack responder gave up, dropping the peer", "user", viewer.ID)
		r.dropLostPeer(viewer)
	}
}

// dropAllPeers empties a room whose shared work panicked.
func (r *Room) dropAllPeers() {
	r.mu.RLock()
	peers := make([]*Peer, 0, len(r.peers))
	for _, peer := range r.peers {
		peers = append(peers, peer)
	}
	r.mu.RUnlock()
	for _, peer := range peers {
		r.dropLostPeer(peer)
	}
}
