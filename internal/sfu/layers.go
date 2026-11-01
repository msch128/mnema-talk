package sfu

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
)

// A screen share can reach the SFU as up to maxLayers separate encodings of
// the same capture ("layers", best first), each on a receive line of its own.
// Every viewer gets exactly one layer on a single outgoing track and can be
// moved from one layer to another without the browser noticing: the SFU
// switches at a keyframe of the new layer and rewrites sequence numbers,
// timestamps and VP8 picture IDs so the viewer sees one continuous stream.
// Nothing is decoded or transcoded, and the sealed payload is untouched.
const maxLayers = 3

// MaxLayers is the most layers a screen share can have.
const MaxLayers = maxLayers

const (
	// keyframeRetry is how often a switch that waits for a keyframe asks the
	// publisher again.
	keyframeRetry = time.Second
	// layerHold keeps a layer nobody receives any more announced as wanted
	// for a while, so a viewer who switches back and forth does not stop and
	// restart the sender's encoder every time.
	layerHold = 8 * time.Second
	// videoClockRate is the RTP clock of every video codec the SFU forwards.
	videoClockRate = 90000
)

// layerClock replaces the clock of timestamp rewriting in tests. It is atomic
// because media goroutines of earlier tests may still read the clock while a
// later test installs its own.
var layerClock atomic.Pointer[func() time.Time]

// layerNow is the clock of timestamp rewriting.
func layerNow() time.Time {
	if now := layerClock.Load(); now != nil {
		return (*now)()
	}
	return time.Now()
}

// Layer describes one encoding of a screen share as its sender declares it.
// The SFU only relays this to the viewers so they can label their choices.
type Layer struct {
	Width  int `json:"width"`
	Height int `json:"height"`
	FPS    int `json:"fps"`
}

// Limits of a declared layer; larger values are refused.
const (
	maxLayerDimension = 16384
	maxLayerFPS       = 240
)

// ErrInvalidLayers is returned for a layer list the SFU refuses.
var ErrInvalidLayers = errors.New("invalid layers")

// ErrNotInRoom is returned when a user who is not a peer of the room tries to
// change what it receives.
var ErrNotInRoom = errors.New("not a member of the room")

func layerDimensionRejected(size int) bool {
	if size < 1 {
		return true
	}
	return size > maxLayerDimension
}

func layerRateRejected(fps int) bool {
	if fps < 1 {
		return true
	}
	return fps > maxLayerFPS
}

func layerShapeRejected(l Layer) bool {
	if layerDimensionRejected(l.Width) || layerDimensionRejected(l.Height) {
		return true
	}
	return layerRateRejected(l.FPS)
}

// validLayers accepts one to maxLayers layers whose pictures get smaller from
// one to the next.
func validLayers(layers []Layer) bool {
	if len(layers) == 0 || len(layers) > maxLayers {
		return false
	}
	for i, l := range layers {
		if layerShapeRejected(l) {
			return false
		}
		if i > 0 && l.Height >= layers[i-1].Height {
			return false
		}
	}
	return true
}

// fitLayerTolerance is the share of the pixels a picture shows that a layer
// must have to be enough for it.
const fitLayerTolerance = 0.9

// fitLayerFor is the index of the smallest layer that still has the pixels a
// picture of `pixels` device pixels in height shows: a small tile gets a small
// layer, a large one the best. 0 while the size is unknown or no layer is
// large enough.
func fitLayerFor(layers []Layer, pixels int) int {
	if len(layers) < 2 || pixels <= 0 {
		return 0
	}
	needed := float64(pixels) * fitLayerTolerance
	for i := len(layers) - 1; i > 0; i-- {
		if float64(layers[i].Height) >= needed {
			return i
		}
	}
	return 0
}

// QualityState is what a viewer currently receives of one screen share.
type QualityState struct {
	// Layer is the layer being forwarded, -1 while none is.
	Layer int
	// Auto is set while the SFU, not the viewer, chooses the layer.
	Auto bool
}

// layerGroup is the set of layers of one capture of a screen share or camera,
// and the viewers receiving it.
type layerGroup struct {
	id        string
	roomID    uuid.UUID
	publisher uuid.UUID
	// source is SourceScreen or SourceCamera.
	source     Source
	notifier   *mediaNotifier
	codec      webrtc.RTPCodecCapability
	kind       codecKind
	superseded atomic.Bool

	// live has bit i set while layer i is attached; declared is the number of
	// layers the sender announced (0 when it did not).
	live     atomic.Uint32
	declared atomic.Int32
	// lastPkt is when a packet of layer i last arrived (unix nanoseconds).
	lastPkt [maxLayers]atomic.Int64
	// sent counts the bytes of layer i that arrived; rate is its bitrate in
	// bits per second over the last measurement (zero when nothing came) and
	// last the most recent bitrate that was not zero, which stays as the size
	// of a layer the sender has switched off. rateAt and seen are the time and
	// the byte counts of the last measurement.
	sent   [maxLayers]atomic.Uint64
	rate   [maxLayers]atomic.Int64
	last   [maxLayers]atomic.Int64
	rateMu sync.Mutex
	seen   [maxLayers]uint64
	rateAt int64
	// sawSPS: layer i sent a sequence parameter set with a keyframe, so an
	// IDR slice alone is not taken for the start of one.
	sawSPS [maxLayers]atomic.Bool

	mu        sync.Mutex
	infos     [maxLayers]*TrackInfo
	viewers   map[uuid.UUID]*viewerTrack
	snapshot  atomic.Pointer[[]*viewerTrack]
	hold      [maxLayers]time.Time
	announced uint8
	started   bool
	timer     *time.Timer
	// wantedOut delivers the announcements to the sharer in order.
	wantedOut newest[[]int]
}

func newLayerGroup(id string, roomID, publisher uuid.UUID, notifier *mediaNotifier, codec webrtc.RTPCodecCapability) *layerGroup {
	return &layerGroup{
		id: id, roomID: roomID, publisher: publisher, notifier: notifier, codec: codec,
		source:  SourceScreen,
		kind:    codecKindOf(codec.MimeType),
		viewers: make(map[uuid.UUID]*viewerTrack),
	}
}

// compatible reports whether another layer can join: the same codec, or a
// viewer would be handed pictures its decoder was not set up for.
func (g *layerGroup) compatible(codec webrtc.RTPCodecCapability) bool {
	return strings.EqualFold(g.codec.MimeType, codec.MimeType) && g.codec.ClockRate == codec.ClockRate
}

// attach registers info as the group's layer info.layer, replacing a layer
// that was there. It reports whether the layer joined.
func (g *layerGroup) attach(info *TrackInfo, codec webrtc.RTPCodecCapability) bool {
	if info.layer < 0 || info.layer >= maxLayers || !g.compatible(codec) {
		return false
	}
	g.mu.Lock()
	if old := g.infos[info.layer]; old != nil {
		old.superseded.Store(true)
	}
	g.infos[info.layer] = info
	g.sawSPS[info.layer].Store(false)
	g.live.Store(g.live.Load() | 1<<info.layer)
	g.mu.Unlock()
	g.retarget()
	return true
}

// detach removes info from its layer slot, if it still holds it.
func (g *layerGroup) detach(info *TrackInfo) {
	g.mu.Lock()
	if g.infos[info.layer] != info {
		g.mu.Unlock()
		return
	}
	g.infos[info.layer] = nil
	g.live.Store(g.live.Load() &^ (1 << info.layer))
	vts := g.viewerListLocked()
	g.mu.Unlock()
	for _, vt := range vts {
		vt.layerGone(info.layer)
	}
	g.retarget()
}

// markLive sets the live bit of info's layer again when its packets resume
// after the share was stopped.
func (g *layerGroup) markLive(info *TrackInfo) {
	g.mu.Lock()
	if g.infos[info.layer] == info && g.live.Load()&(1<<info.layer) == 0 {
		g.live.Store(g.live.Load() | 1<<info.layer)
		g.mu.Unlock()
		g.retarget()
		return
	}
	g.mu.Unlock()
}

// shareStopped forgets what the stopped share announced and had live, so a
// restart is announced to the sender afresh and no silent layer is resolved.
func (g *layerGroup) shareStopped() {
	g.mu.Lock()
	g.resetAnnouncedLocked()
	g.hold = [maxLayers]time.Time{}
	g.live.Store(0)
	g.declared.Store(0)
	g.mu.Unlock()
	g.retarget()
}

// resetAnnouncedLocked makes the next refreshWanted announce its mask.
func (g *layerGroup) resetAnnouncedLocked() {
	g.started = false
	g.announced = 0
	g.stopTimerLocked()
}

// stopTimerLocked cancels the pending hold timer, if any. g.mu is held.
func (g *layerGroup) stopTimerLocked() {
	if g.timer != nil {
		g.timer.Stop()
		g.timer = nil
	}
}

// flowing is the layer nearest to want that delivered a packet within the
// last second; -1 when there is none. Worse layers win ties.
func (g *layerGroup) flowing(want int, now time.Time) int {
	recent := func(i int) bool {
		return i >= 0 && i < maxLayers && g.live.Load()&(1<<i) != 0 &&
			now.UnixNano()-g.lastPkt[i].Load() < int64(time.Second)
	}
	for d := 0; d < maxLayers; d++ {
		if recent(want + d) {
			return want + d
		}
		if recent(want - d) {
			return want - d
		}
	}
	return -1
}

// supersede ends the group: its layers stop forwarding.
func (g *layerGroup) supersede() {
	g.superseded.Store(true)
	g.mu.Lock()
	for _, info := range g.infos {
		if info != nil {
			info.superseded.Store(true)
		}
	}
	g.mu.Unlock()
}

// close stops the group's timer.
func (g *layerGroup) close() {
	g.mu.Lock()
	g.stopTimerLocked()
	g.mu.Unlock()
}
