// Package sfu is a selective forwarding unit on Pion WebRTC: every peer sends
// its tracks once and the SFU forwards the RTP to everyone else in the room,
// without transcoding.
package sfu

import (
	"context"
	"github.com/msch128/mnema-talk/internal/safego"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/pion/ice/v4"
	"github.com/pion/webrtc/v4"
)

// keyframeMinInterval rate-limits keyframe requests per forwarded track, so
// a burst of PLIs from several viewers costs the publisher one keyframe.
const keyframeMinInterval = 300 * time.Millisecond

// CameraStreamPrefix precedes the publisher's user ID in a camera's stream ID.
const CameraStreamPrefix = "cam:"

// ScreenAudioStreamPrefix precedes the publisher's user ID in the stream ID
// of a screen share's sound.
const ScreenAudioStreamPrefix = "screen:"

// defaultOfferTimeout is how long an offer may stay unanswered before the SFU
// sends it again. An offer or answer can get lost (a dropped WebSocket frame,
// a client that failed to apply the offer); without the retry the peer would
// stay in have-local-offer and never negotiate again. Pion cannot roll back a
// local offer, so the same offer is repeated, at most maxOfferResends times.
const defaultOfferTimeout = 10 * time.Second

const maxOfferResends = 6

// An offer that cannot be built is tried again after offerRetryDelay, and the
// peer is dropped after maxOfferBuildFailures failures in a row.
const (
	offerRetryDelay       = 250 * time.Millisecond
	maxOfferBuildFailures = 5
)

// resumeHoldoff keeps late packets of a stopped video from re-publishing it.
const resumeHoldoff = 500 * time.Millisecond

// TrackInfo is one forwarded track: the publisher's remote track and the
// local track every other peer subscribes to.
type TrackInfo struct {
	// Track is what every subscriber binds to. A screen share's layers have no
	// shared track: each viewer gets one of its own from the group.
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
	// A replaced capture must never resume or forward alongside its successor.
	superseded atomic.Bool
	// writeErrLogged keeps a failing forward from logging every packet.
	writeErrLogged atomic.Bool
	// group and layer are set on a screen share received as layers (see
	// layerGroup); layer 0 is the best.
	group *layerGroup
	layer int
}

// codecMime is the codec the track carries, for the logs.
func (t *TrackInfo) codecMime() string {
	switch {
	case t.Track != nil:
		return t.Track.Codec().MimeType
	case t.group != nil:
		return t.group.codec.MimeType
	}
	return ""
}

// isSuperseded reports whether a newer capture replaced this track.
func (t *TrackInfo) isSuperseded() bool {
	return t.superseded.Load() || t.group != nil && t.group.superseded.Load()
}

// sameCapture reports whether two tracks belong to one capture: the same
// track, or layers of one screen share.
func (t *TrackInfo) sameCapture(o *TrackInfo) bool {
	return t == o || t.group != nil && t.group == o.group
}

type Peer struct {
	// denied is the SourceSet the user may not publish.
	denied    atomic.Uint32
	ID        uuid.UUID
	PC        *webrtc.PeerConnection
	SendOffer func(offer webrtc.SessionDescription)
	SendICE   func(candidate *webrtc.ICECandidateInit)
	room      *Room

	// negotiationPending is set when tracks changed while an offer was still
	// unanswered; the next answer triggers another round.
	negotiationPending atomic.Bool
	// offerSent is guarded by signalingMu. Reading LocalDescription may wait for
	// the ICE task loop, so it must not be used as a flag under the room lock.
	offerSent bool
	// needOffer and offerFailures track an offer that could not be built (see
	// negotiateOrRetry); guarded by signalingMu.
	needOffer     bool
	offerFailures int
	// offerFault makes building an offer fail (tests only).
	offerFault func() error
	// candMu guards cands: trickle candidates held back until the offer was
	// handed over (outgoing) or the answer was applied (incoming).
	candMu sync.Mutex
	cands  candidateState
	// allowLoopbackCands admits loopback candidates of the client; set when
	// the server itself is announced on loopback (dev, e2e).
	allowLoopbackCands bool
	// signalingMu permits one active signaling task per peer.
	signalingMu sync.Mutex
	// offerGen identifies the latest offer; a watchdog for an older one
	// does nothing.
	offerGen atomic.Uint64
	// addTrackFailed holds the tracks that could not be added to this peer,
	// so each failure is logged once rather than every signaling round.
	// Guarded by signalingMu.
	addTrackFailed map[string]bool
	// viewerTracks are this peer's outgoing tracks of layered screen shares by
	// track ID, so they can be released with the track.
	vtMu         sync.Mutex
	viewerTracks map[string]*viewerTrack
	// lastCapped is the set of cameras last announced as not forwarded (see
	// announceCapped); guarded by signalingMu.
	lastCapped []uuid.UUID
	// vtReleased is set by releaseViewerTracks; no viewer track is added after.
	vtReleased bool
	// gen orders this peer against the earlier and later peers of the same
	// user (see layerGroup.addViewer); attachStep is a test seam called
	// between the steps of addSender.
	gen uint64
	// connID is the ID Pion gave the connection, which is what the
	// connection's viewerGuard knows of it.
	connID string
	// hubConn is the id of the hub connection that made the peer, and log
	// carries it as conn_id (see nameConn).
	hubConn    string
	log        *slog.Logger
	attachStep func(step string)
	// lostMu guards onLost and lost (see OnLost).
	lostMu sync.Mutex
	onLost func()
	lost   bool
}

type Room struct {
	allowLoopbackCands bool
	load               *peerLoad
	ID                 uuid.UUID
	// conns is the SFU's record of hub connections (nil in a bare room).
	conns *connNames
	// peers maps user → their current connection; a rejoin replaces it.
	peers map[uuid.UUID]*Peer
	// trackLocals is keyed by the local track ID, which is scoped to the
	// publishing user (see trackKey) so clients cannot collide.
	trackLocals map[string]*TrackInfo
	// Keeps the latest source even while stopped, so late packets from an older
	// capture cannot replace a newer track. At most four entries per publisher.
	latestSources map[string]*TrackInfo
	// groups holds each publisher's screen share received as layers, guarded
	// by mu.
	groups map[uuid.UUID]*layerGroup
	// cameraGroups is the same for a camera sent as layers; cameraLayerMeta
	// is what each publisher declared about them. Both guarded by mu. A camera
	// without declared layers is forwarded as one plain track, as before.
	cameraGroups    map[uuid.UUID]*layerGroup
	cameraLayerMeta map[uuid.UUID][]Layer
	mu              sync.RWMutex
	iceServers      []webrtc.ICEServer
	offerTimeout    time.Duration

	// subs is what each viewer receives (see Subscriptions); guarded by mu.
	subs map[uuid.UUID]*Subscriptions
	// lastMedia is the announced screen/camera state per publisher; guarded by mu.
	lastMedia map[uuid.UUID]MediaState
	// lastViewers is the announced non-empty audience of each screen share
	// (see notifyMedia); guarded by mu.
	lastViewers map[uuid.UUID][]uuid.UUID
	// layerMeta is what each publisher declared about its screen share's
	// layers; lastLayers what was announced. Both guarded by mu.
	layerMeta  map[uuid.UUID][]Layer
	lastLayers map[uuid.UUID][]Layer
	notifyMu   sync.Mutex
	notifier   *mediaNotifier
	// iceSeen hears of every connection that completed ICE; nil in tests.
	iceSeen *iceObserver
}

type SFU struct {
	// announcers tracks the KeepAnnounceCurrent loops (tests wait for them).
	announcers sync.WaitGroup
	load       *peerLoad
	apiMu      sync.RWMutex
	api        *webrtc.API
	// announced are the addresses offered to browsers (see SetAnnouncedIPs).
	announced []string
	portMin   uint16
	portMax   uint16
	udpMux    ice.UDPMux
	closed    atomic.Bool
	closeOnce sync.Once
	closeErr  error

	// afterBuild runs in Join right after the connection was built
	// (tests only).
	afterBuild func()

	iceServers []webrtc.ICEServer
	// offerTimeout is defaultOfferTimeout; tests shorten it.
	offerTimeout time.Duration
	rooms        map[uuid.UUID]*Room
	// conns names the hub connection of each of Pion's connections.
	conns connNames
	// The upload governor (StartUploadGovernor): its stop function, the
	// goroutine to wait for and how many times one was started.
	uploadMu   sync.Mutex
	uploadStop context.CancelFunc
	uploadWG   sync.WaitGroup
	uploadRuns atomic.Int32
	roomsMu    sync.Mutex
	notifier   *mediaNotifier
	iceSeen    *iceObserver
	// probes counts the live connection tests (see Probe).
	probeMu  sync.Mutex
	probes   int
	probesBy map[string]int
}

// connIDAttr names, on a peer's log lines, the hub connection that made the
// peer (Join's connID; the hub logs the same attribute). A member's second
// session and the peer a rejoin replaced are told apart by it.
const connIDAttr = "conn_id"

// nameConn records the hub connection behind the peer and binds it to the
// peer's logger once, so no log call builds the attribute again.
func (p *Peer) nameConn(id string) {
	if id == "" {
		return
	}
	p.hubConn = id
	p.log = slog.With(connIDAttr, id)
}

// logger is the peer's logger. A peer without a hub connection (tests, the
// load generator) logs like the rest of the server.
func (p *Peer) logger() *slog.Logger {
	if p == nil || p.log == nil {
		return slog.Default()
	}
	return p.log
}

// panics is the hub connection's id for the panic line of the peer's
// goroutines, timers and callbacks.
func (p *Peer) panics() safego.Conn {
	if p == nil {
		return ""
	}
	return safego.Conn(p.hubConn)
}

// logger is the logger of the peer that publishes the track.
func (t *TrackInfo) logger() *slog.Logger { return t.publisher.logger() }

// connNames knows the hub connection of each of Pion's connections, for the
// one site that has only Pion's ID when it panics (viewerGuard). An entry
// lives from the peer's attach until its connection's interceptors close.
type connNames struct{ byPionID sync.Map }

func (n *connNames) name(pionID, hubConn string) {
	if n != nil && hubConn != "" {
		n.byPionID.Store(pionID, hubConn)
	}
}

func (n *connNames) of(pionID string) safego.Conn {
	if n == nil {
		return ""
	}
	id, _ := n.byPionID.Load(pionID)
	s, _ := id.(string)
	return safego.Conn(s)
}

func (n *connNames) forget(pionID string) {
	if n != nil {
		n.byPionID.Delete(pionID)
	}
}
