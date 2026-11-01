package sfu

import (
	"errors"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/metrics"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

// viewerTrack is the outgoing track of one viewer for a layered screen share.
// It embeds a TrackLocalStaticRTP of its own, which Pion binds and writes to
// like any other, so codec matching, SSRC and payload type stay Pion's
// business; what this adds is choosing which layer's packets reach it and
// keeping their numbering continuous across layer switches.
type viewerTrack struct {
	*webrtc.TrackLocalStaticRTP
	group  *layerGroup
	viewer uuid.UUID

	// wantA and curA mirror want and cur for readers without the lock.
	wantA atomic.Int32
	curA  atomic.Int32

	mu sync.Mutex
	// pref is the viewer's choice (-1 automatic), want the layer it stands
	// for, eff the layer that exists nearest to it and cur the layer being
	// forwarded (-1 before the first keyframe).
	pref, want, eff, cur int
	// fit is the best layer the viewer's picture can use (0 is the best the
	// share has), which automatic never exceeds.
	fit int
	// ad chooses the layer of a viewer who left it to the server (pref < 0).
	ad         *adapter
	closed     bool
	started    bool
	lastKeyAsk time.Time
	// since is when the track was made, the stall clock until a first packet.
	since time.Time
	// Offsets that map the current layer's numbers onto the viewer's stream.
	seqOff uint16
	tsOff  uint32
	picOff uint16
	tl0Off uint8
	// The last numbers sent to the viewer.
	lastSeq uint16
	lastTs  uint32
	lastAt  time.Time
	lastPic uint16
	lastTl0 uint8
	scratch []byte
	// out is the packet handed to the sink: the source with this viewer's numbers.
	out rtp.Packet
	// sink delivers a packet to the viewer: the track's own WriteRTP.
	sink func(*rtp.Packet) error
	// reported is the last state announced to the viewer.
	reported   QualityState
	hasRep     bool
	qualityOut newest[QualityState]
}

// close ends delivery; the track itself is torn down by Pion.
func (vt *viewerTrack) close() {
	vt.mu.Lock()
	vt.closed = true
	vt.mu.Unlock()
}

// retarget re-resolves the layer this viewer should receive. A layer change
// that waits for a keyframe asks the publisher for one.
func (vt *viewerTrack) retarget() {
	vt.mu.Lock()
	if vt.ad != nil {
		vt.ad.setFloor(vt.group.floor())
		vt.want = vt.ad.layer
		vt.wantA.Store(int32(vt.want))
	}
	vt.eff = vt.group.resolveFlowing(vt.want, layerNow())
	ask := vt.keyframeDueLocked(layerNow())
	eff := vt.eff
	vt.mu.Unlock()
	if ask {
		vt.group.requestKeyframe(eff)
	}
}

// setPref applies the viewer's choice: -1 automatic, else a layer index.
func (vt *viewerTrack) setPref(pref int) {
	vt.mu.Lock()
	vt.pref = pref
	switch {
	case pref >= 0:
		vt.ad = nil
	case vt.ad == nil:
		// Automatic carries on from the layer chosen so far.
		vt.ad = newAdapter(layerNow(), vt.group.floor())
		vt.ad.best = vt.fit
		vt.ad.layer = max(min(vt.want, vt.ad.floor), vt.ad.top())
		// Back to automatic from a worse layer than the picture can use: climb
		// at the pace of an enlarged picture, not after a long clean stretch.
		vt.ad.climbing = vt.ad.layer > vt.ad.top()
	}
	vt.want = vt.chosenLayer()
	vt.wantA.Store(int32(vt.want))
	vt.lastKeyAsk = time.Time{}
	vt.mu.Unlock()
	vt.retarget()
	vt.group.refreshWanted()
	vt.report(true)
}

// setFit tells the track how good a layer the viewer's picture can use: the
// best layer whose size is not more than the picture shows. Automatic stays
// at or below it, which saves the bandwidth of a small tile, and climbs when
// the picture grows.
func (vt *viewerTrack) setFit(fit int) {
	fit = max(fit, 0)
	vt.mu.Lock()
	vt.fit = fit
	changed := false
	if vt.ad != nil && !vt.closed {
		vt.ad.setFloor(vt.group.floor())
		changed = vt.ad.setBest(layerNow(), fit)
		vt.want = vt.ad.layer
		vt.wantA.Store(int32(vt.want))
	}
	vt.mu.Unlock()
	if changed {
		vt.retarget()
	}
	vt.group.refreshWanted()
}

// chosenLayer is the layer the viewer's choice stands for: the one it picked,
// or the one the adapter has arrived at.
func (vt *viewerTrack) chosenLayer() int {
	if vt.ad != nil {
		return vt.ad.layer
	}
	return vt.pref
}

// observeReport gives a receiver report of the viewer to the automatic choice.
func (vt *viewerTrack) observeReport(loss float64, rtt time.Duration) {
	vt.observeReportWithBandwidth(loss, rtt, 0)
}

// observeReportWithBandwidth is observeReport with the bandwidth estimate of
// the viewer's connection in bits per second (zero when unknown).
func (vt *viewerTrack) observeReportWithBandwidth(loss float64, rtt time.Duration, bandwidth int) {
	now := layerNow()
	vt.retryKeyframe(now)
	vt.mu.Lock()
	if vt.ad == nil || vt.closed {
		vt.mu.Unlock()
		return
	}
	vt.ad.setBandwidth(vt.bandwidthSampleLocked(now, bandwidth))
	layer, changed := vt.ad.observe(now, loss, rtt)
	if changed && vt.ad.narrowed {
		metrics.SFUBandwidthMoves.Inc()
	}
	if !changed {
		changed = vt.stalledLocked(now)
		layer = vt.ad.layer
	}
	if changed {
		vt.want = layer
		vt.wantA.Store(int32(layer))
	}
	vt.mu.Unlock()
	if changed {
		vt.retarget()
		vt.group.refreshWanted()
	}
}

// bandwidthSampleLocked pairs the estimate with the bitrates of the layer
// being received and the next better one, or reports nothing when any is
// unknown or the viewer is not on the layer the adapter stands on.
func (vt *viewerTrack) bandwidthSampleLocked(now time.Time, est int) (int, int, int) {
	if est <= 0 || vt.cur < 0 || vt.cur != vt.ad.layer {
		return 0, 0, 0
	}
	metrics.SFUBandwidth.With(metrics.BandwidthRange(est)).Inc()
	g := vt.group
	g.sampleRates(now)
	next := 0
	if vt.cur > 0 {
		next = int(g.last[vt.cur-1].Load())
	}
	return est, int(g.rate[vt.cur].Load()), next
}

// stalledLocked moves an automatic viewer to a layer that delivers when
// nothing has reached it for adaptStall (no packets mean no loss to report).
func (vt *viewerTrack) stalledLocked(now time.Time) bool {
	base := vt.lastAt
	if base.IsZero() {
		base = vt.since
	}
	if now.Sub(base) < adaptStall {
		return false
	}
	to := vt.group.flowing(vt.ad.layer, now)
	if to < 0 {
		return false
	}
	return vt.ad.stalledTo(now, to)
}

// layerGone drops the viewer's current layer when it was idx.
func (vt *viewerTrack) layerGone(idx int) {
	vt.mu.Lock()
	if vt.cur == idx {
		vt.cur = -1
		vt.curA.Store(-1)
		// The picture stops: the next layer is asked for at once.
		vt.lastKeyAsk = time.Time{}
	}
	vt.mu.Unlock()
}

// state is what the viewer currently receives.
func (vt *viewerTrack) state() QualityState {
	vt.mu.Lock()
	defer vt.mu.Unlock()
	return QualityState{Layer: vt.cur, Auto: vt.pref < 0}
}

// report tells the viewer what it receives when that changed (or always with
// force).
func (vt *viewerTrack) report(force bool) {
	// The state is read, recorded and queued under one hold, so concurrent
	// reports reach the viewer in the order their states were taken. Queueing
	// only stores the value; delivery runs on its own goroutine.
	vt.mu.Lock()
	defer vt.mu.Unlock()
	st := QualityState{Layer: vt.cur, Auto: vt.pref < 0}
	same := vt.hasRep && vt.reported == st
	vt.reported, vt.hasRep = st, true
	if vt.closed || (same && !force) {
		return
	}
	vt.group.notifyQuality(vt, st)
}

// write offers one packet of layer idx to the viewer.
func (vt *viewerTrack) write(idx int, src *rtp.Packet) {
	now := layerNow()
	vt.mu.Lock()
	switched, ask, eff := vt.writeLocked(idx, src, now)
	vt.mu.Unlock()
	if ask {
		vt.group.requestKeyframe(eff)
	}
	if switched {
		vt.group.refreshWanted()
		vt.report(false)
	}
}

func (vt *viewerTrack) checkLayerTransition(idx int, src *rtp.Packet, now time.Time) (drop bool, switched bool, ask bool) {
	eff := vt.eff
	if idx != vt.cur {
		if idx != eff {
			return true, false, false
		}
		if !vt.group.keyStart(idx, src) {
			return true, false, vt.keyframeDueLocked(now)
		}
		vt.switchLocked(idx, src, now)
		return false, true, false
	}
	if eff != vt.cur {
		return false, false, vt.keyframeDueLocked(now)
	}
	return false, false, false
}

func (vt *viewerTrack) sendPacketLocked(src *rtp.Packet, now time.Time) {
	// The viewer's copy is a field, not a local: the sink is a func value, so
	// a local would be allocated on the heap for every packet and viewer. The
	// sink returns before the next packet (the track copies what it keeps),
	// and vt.mu is held throughout.
	vt.out = *src
	out := &vt.out
	out.Header.SequenceNumber = src.SequenceNumber + vt.seqOff
	out.Header.Timestamp = src.Timestamp + vt.tsOff
	if vt.group.kind == codecVP8 {
		vt.patchVP8Locked(out, src)
	}
	vt.lastSeq, vt.lastTs, vt.lastAt = out.Header.SequenceNumber, out.Header.Timestamp, now
	if err := vt.sink(out); err != nil && !errors.Is(err, io.ErrClosedPipe) {
		slog.Debug("sfu layer write", "viewer", vt.viewer, "err", err)
	}
}

func (vt *viewerTrack) writeLocked(idx int, src *rtp.Packet, now time.Time) (switched, ask bool, eff int) {
	if vt.closed {
		return false, false, -1
	}
	eff = vt.eff
	drop, switched, ask := vt.checkLayerTransition(idx, src, now)
	if drop {
		return false, ask, eff
	}
	vt.sendPacketLocked(src, now)
	return switched, ask, eff
}

// switchLocked makes idx the layer being forwarded, starting at the keyframe
// packet src: the numbers of the stream the viewer sees carry on from the last
// ones sent.
func (vt *viewerTrack) switchLocked(idx int, src *rtp.Packet, now time.Time) {
	if vt.started {
		if vt.group.source == SourceCamera {
			metrics.SFUCameraLayerSwitches.Inc()
		} else {
			metrics.SFULayerSwitches.Inc()
		}
		vt.seqOff = vt.lastSeq + 1 - src.SequenceNumber
		ticks := uint32(now.Sub(vt.lastAt).Seconds() * videoClockRate)
		if ticks < 1 {
			ticks = 1
		}
		vt.tsOff = vt.lastTs + ticks - src.Timestamp
		vt.picOff, vt.tl0Off = 0, 0
		if vt.group.kind == codecVP8 {
			vt.carryVP8OffsetsLocked(src)
		}
	}
	vt.started = true
	vt.cur = idx
	vt.curA.Store(int32(idx))
}

// carryVP8OffsetsLocked sets the picture ID and TL0PICIDX offsets so the
// keyframe packet src continues the numbering the viewer last saw.
func (vt *viewerTrack) carryVP8OffsetsLocked(src *rtp.Packet) {
	d, ok := parseVP8(src.Payload)
	if !ok {
		return
	}
	if d.picPos >= 0 {
		vt.picOff = vt.lastPic + 1 - d.pic
	}
	if d.tl0Pos >= 0 {
		vt.tl0Off = vt.lastTl0 + 1 - d.tl0
	}
}

// patchVP8Locked remembers the last picture ID and TL0PICIDX sent and, when
// the offsets are non-zero, rewrites them in out's payload (a copy of src).
func (vt *viewerTrack) patchVP8Locked(out, src *rtp.Packet) {
	d, ok := parseVP8(src.Payload)
	if !ok {
		return
	}
	if d.picPos >= 0 {
		vt.lastPic = d.pic + vt.picOff
		if d.pic16 {
			vt.lastPic &= 0x7fff
		} else {
			vt.lastPic &= 0x7f
		}
	}
	if d.tl0Pos >= 0 {
		vt.lastTl0 = d.tl0 + vt.tl0Off
	}
	if vt.picOff != 0 || vt.tl0Off != 0 {
		vt.scratch = patchVP8(vt.scratch, src.Payload, d, vt.picOff, vt.tl0Off)
		out.Payload = vt.scratch
	}
}
