// Package sfu is a selective forwarding unit on Pion WebRTC: every peer sends
// its tracks once and the SFU forwards the RTP to everyone else in the room,
// without transcoding.
package sfu

import (
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
	// lostMu guards onLost and lost (see OnLost).
	lostMu sync.Mutex
	onLost func()
	lost   bool
}

type Room struct {
	allowLoopbackCands bool
	load               *peerLoad
	ID                 uuid.UUID
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
	roomsMu      sync.Mutex
	notifier     *mediaNotifier
	iceSeen      *iceObserver
	// probes counts the live connection tests (see Probe).
	probeMu  sync.Mutex
	probes   int
	probesBy map[string]int
}
