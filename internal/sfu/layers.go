package sfu

import (
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/pion/rtp"
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

// layerNow is the clock of timestamp rewriting; tests replace it.
var layerNow = time.Now

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

// validLayers accepts one to maxLayers layers whose pictures get smaller from
// one to the next.
func validLayers(layers []Layer) bool {
	if len(layers) == 0 || len(layers) > maxLayers {
		return false
	}
	for i, l := range layers {
		if l.Width < 1 || l.Width > maxLayerDimension || l.Height < 1 || l.Height > maxLayerDimension || l.FPS < 1 || l.FPS > maxLayerFPS {
			return false
		}
		if i > 0 && l.Height >= layers[i-1].Height {
			return false
		}
	}
	return true
}

// QualityState is what a viewer currently receives of one screen share.
type QualityState struct {
	// Layer is the layer being forwarded, -1 while none is.
	Layer int
	// Auto is set while the SFU, not the viewer, chooses the layer.
	Auto bool
}

// layerGroup is the set of layers of one capture of a screen share, and the
// viewers receiving it.
type layerGroup struct {
	id         string
	roomID     uuid.UUID
	publisher  uuid.UUID
	notifier   *mediaNotifier
	codec      webrtc.RTPCodecCapability
	kind       codecKind
	superseded atomic.Bool

	// live has bit i set while layer i is attached; declared is the number of
	// layers the sender announced (0 when it did not).
	live     atomic.Uint32
	declared atomic.Int32
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

func (g *layerGroup) viewerListLocked() []*viewerTrack {
	out := make([]*viewerTrack, 0, len(g.viewers))
	for _, vt := range g.viewers {
		out = append(out, vt)
	}
	return out
}

func (g *layerGroup) publishViewersLocked() {
	list := g.viewerListLocked()
	g.snapshot.Store(&list)
}

// representative is a live layer's info, the best one; nil when none is live.
func (g *layerGroup) representative() *TrackInfo {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, info := range g.infos {
		if info != nil {
			return info
		}
	}
	return nil
}

// newViewer makes the outgoing track one viewer gets for the group. pref is
// the viewer's choice, -1 for automatic. It receives nothing until addViewer.
func (g *layerGroup) newViewer(viewer uuid.UUID, pref int, trackID, streamID string) *viewerTrack {
	// NewTrackLocalStaticRTP never fails.
	local, _ := webrtc.NewTrackLocalStaticRTP(g.codec, trackID, streamID)
	vt := &viewerTrack{TrackLocalStaticRTP: local, group: g, viewer: viewer, pref: pref, cur: -1, eff: -1, sink: local.WriteRTP}
	if pref < 0 {
		vt.ad = newAdapter(layerNow(), g.floor())
	}
	vt.want = vt.chosenLayer()
	vt.curA.Store(-1)
	vt.wantA.Store(int32(vt.want))
	return vt
}

// addViewer starts delivering to vt and asks for the keyframe it waits for.
func (g *layerGroup) addViewer(vt *viewerTrack) {
	g.mu.Lock()
	old := g.viewers[vt.viewer]
	g.viewers[vt.viewer] = vt
	g.publishViewersLocked()
	g.mu.Unlock()
	if old != nil && old != vt {
		old.close()
	}
	vt.retarget()
	g.refreshWanted()
}

// removeViewer stops delivering to vt.
func (g *layerGroup) removeViewer(vt *viewerTrack) {
	g.mu.Lock()
	if g.viewers[vt.viewer] == vt {
		delete(g.viewers, vt.viewer)
		g.publishViewersLocked()
	}
	g.mu.Unlock()
	vt.close()
	g.refreshWanted()
}

// viewerFor is viewer's outgoing track of the group, nil if it has none.
func (g *layerGroup) viewerFor(viewer uuid.UUID) *viewerTrack {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.viewers[viewer]
}

// write hands one packet of layer idx to every viewer.
func (g *layerGroup) write(idx int, p *rtp.Packet) {
	list := g.snapshot.Load()
	if list == nil {
		return
	}
	for _, vt := range *list {
		vt.write(idx, p)
	}
}

// keyStart reports whether p starts a keyframe of layer idx, the only place
// where a viewer may join that layer.
func (g *layerGroup) keyStart(idx int, p *rtp.Packet) bool {
	switch g.kind {
	case codecVP8:
		d, ok := parseVP8(p.Payload)
		return ok && d.key
	case codecH264:
		sps, idr := h264Start(p.Payload)
		if sps {
			g.sawSPS[idx].Store(true)
			return true
		}
		return idr && !g.sawSPS[idx].Load()
	}
	// A codec the SFU cannot read is forwarded as a single layer.
	return idx == 0
}

// requestKeyframe asks the publisher for a keyframe of layer idx.
func (g *layerGroup) requestKeyframe(idx int) {
	if idx < 0 || idx >= maxLayers {
		return
	}
	g.mu.Lock()
	info := g.infos[idx]
	g.mu.Unlock()
	if info != nil {
		info.requestKeyframe()
	}
}

// resolve turns a wished-for layer into one that exists: the lowest quality
// at or above it, else the best one below. -1 when no layer is attached.
func (g *layerGroup) resolve(want int) int {
	live := g.live.Load()
	want = g.clampWant(want)
	for i := want; i >= 0; i-- {
		if live&(1<<i) != 0 {
			return i
		}
	}
	for i := want + 1; i < maxLayers; i++ {
		if live&(1<<i) != 0 {
			return i
		}
	}
	return -1
}

// floor is the worst layer a viewer may be given: the last declared one, else
// the lowest-quality layer that exists.
func (g *layerGroup) floor() int {
	if n := int(g.declared.Load()); n > 0 {
		return min(n, maxLayers) - 1
	}
	live := g.live.Load()
	for i := maxLayers - 1; i > 0; i-- {
		if live&(1<<i) != 0 {
			return i
		}
	}
	return 0
}

// clampWant is a wished-for layer limited to the declared ones.
func (g *layerGroup) clampWant(want int) int {
	if n := int(g.declared.Load()); n > 0 && want >= n {
		return n - 1
	}
	if want >= maxLayers {
		return maxLayers - 1
	}
	return want
}

// setDeclared records how many layers the sender announced.
func (g *layerGroup) setDeclared(n int) {
	g.declared.Store(int32(n))
	g.retarget()
	g.refreshWanted()
}

// retarget moves every viewer to the layer it should have now.
func (g *layerGroup) retarget() {
	g.mu.Lock()
	vts := g.viewerListLocked()
	g.mu.Unlock()
	for _, vt := range vts {
		vt.retarget()
	}
}

// need is the layers some viewer receives or is about to.
func (g *layerGroup) need() uint8 {
	g.mu.Lock()
	vts := g.viewerListLocked()
	g.mu.Unlock()
	var mask uint8
	for _, vt := range vts {
		if want := g.clampWant(int(vt.wantA.Load())); want >= 0 {
			mask |= 1 << want
		}
		if cur := vt.curA.Load(); cur >= 0 {
			mask |= 1 << cur
		}
	}
	if len(vts) == 0 {
		// Nobody watches: the sender keeps its best layer, as before layers.
		mask = 1
	}
	return mask
}

// refreshWanted tells the publisher which layers to send when that changed.
// A layer stays wanted for layerHold after the last viewer left it.
func (g *layerGroup) refreshWanted() {
	need := g.need()
	now := layerNow()
	g.mu.Lock()
	var mask uint8
	var next time.Time
	for i := range g.hold {
		switch {
		case need&(1<<i) != 0:
			g.hold[i] = now.Add(layerHold)
			mask |= 1 << i
		case g.hold[i].After(now):
			mask |= 1 << i
			if next.IsZero() || g.hold[i].Before(next) {
				next = g.hold[i]
			}
		}
	}
	changed := !g.started || mask != g.announced
	g.started = true
	g.announced = mask
	if g.timer != nil {
		g.timer.Stop()
		g.timer = nil
	}
	if !next.IsZero() && !g.superseded.Load() {
		g.timer = time.AfterFunc(next.Sub(now)+time.Millisecond, g.refreshWanted)
	}
	g.mu.Unlock()
	if changed && !g.superseded.Load() {
		g.notifyWanted(mask)
	}
}

// close stops the group's timer.
func (g *layerGroup) close() {
	g.mu.Lock()
	if g.timer != nil {
		g.timer.Stop()
		g.timer = nil
	}
	g.mu.Unlock()
}

func (g *layerGroup) notifyWanted(mask uint8) {
	if g.notifier == nil {
		return
	}
	fn := g.notifier.wantedFn()
	if fn == nil {
		return
	}
	layers := make([]int, 0, maxLayers)
	for i := 0; i < maxLayers; i++ {
		if mask&(1<<i) != 0 {
			layers = append(layers, i)
		}
	}
	g.wantedOut.send(layers, func(layers []int) { fn(g.roomID, g.publisher, layers) })
}

func (g *layerGroup) notifyQuality(vt *viewerTrack, st QualityState) {
	if g.notifier == nil {
		return
	}
	if fn := g.notifier.qualityFn(); fn != nil {
		vt.qualityOut.send(st, func(st QualityState) { fn(g.roomID, vt.viewer, g.publisher, st) })
	}
}

// layerWriter feeds one layer's packets into its group. It is the "local
// track" of that layer's forwarding loop.
type layerWriter struct {
	group *layerGroup
	layer int
}

func (w layerWriter) WriteRTP(p *rtp.Packet) error {
	w.group.write(w.layer, p)
	return nil
}

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
	// ad chooses the layer of a viewer who left it to the server (pref < 0).
	ad         *adapter
	closed     bool
	started    bool
	lastKeyAsk time.Time
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
	vt.eff = vt.group.resolve(vt.want)
	ask := vt.keyframeDueLocked(layerNow())
	eff := vt.eff
	vt.mu.Unlock()
	if ask {
		vt.group.requestKeyframe(eff)
	}
}

// keyframeDueLocked reports whether a keyframe of the target layer should be
// asked for now (and records the ask).
func (vt *viewerTrack) keyframeDueLocked(now time.Time) bool {
	if vt.closed || vt.eff < 0 || vt.eff == vt.cur || now.Sub(vt.lastKeyAsk) < keyframeRetry {
		return false
	}
	vt.lastKeyAsk = now
	return true
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
		vt.ad.layer = min(vt.want, vt.ad.floor)
	}
	vt.want = vt.chosenLayer()
	vt.wantA.Store(int32(vt.want))
	vt.lastKeyAsk = time.Time{}
	vt.mu.Unlock()
	vt.retarget()
	vt.group.refreshWanted()
	vt.report(true)
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
	now := layerNow()
	vt.mu.Lock()
	if vt.ad == nil || vt.closed {
		vt.mu.Unlock()
		return
	}
	layer, changed := vt.ad.observe(now, loss, rtt)
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

// viewerKeyframe passes on the viewer's own keyframe request; for an automatic
// viewer, repeated requests are a sign that it cannot decode what it gets.
func (vt *viewerTrack) viewerKeyframe() {
	now := layerNow()
	vt.mu.Lock()
	if vt.ad != nil {
		vt.ad.keyframeRequested(now)
	}
	vt.mu.Unlock()
	vt.requestKeyframe()
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

// requestKeyframe passes the viewer's own keyframe request to the layer it
// receives (or is about to).
func (vt *viewerTrack) requestKeyframe() {
	vt.mu.Lock()
	idx := vt.cur
	if idx < 0 {
		idx = vt.eff
	}
	vt.mu.Unlock()
	vt.group.requestKeyframe(idx)
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
	st := vt.state()
	vt.mu.Lock()
	same := vt.hasRep && vt.reported == st
	vt.reported, vt.hasRep = st, true
	closed := vt.closed
	vt.mu.Unlock()
	if closed || (same && !force) {
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

func (vt *viewerTrack) writeLocked(idx int, src *rtp.Packet, now time.Time) (switched, ask bool, eff int) {
	if vt.closed {
		return false, false, -1
	}
	eff = vt.eff
	if idx != vt.cur {
		if idx != eff {
			return false, false, eff
		}
		if !vt.group.keyStart(idx, src) {
			return false, vt.keyframeDueLocked(now), eff
		}
		vt.switchLocked(idx, src, now)
		switched = true
	} else if eff != vt.cur {
		// A switch is pending; the current layer's packets are the only
		// ones flowing, so they carry the reminder to ask again.
		ask = vt.keyframeDueLocked(now)
	}

	out := *src
	out.Header.SequenceNumber = src.SequenceNumber + vt.seqOff
	out.Header.Timestamp = src.Timestamp + vt.tsOff
	if vt.group.kind == codecVP8 {
		if d, ok := parseVP8(src.Payload); ok {
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
	}
	vt.lastSeq, vt.lastTs, vt.lastAt = out.Header.SequenceNumber, out.Header.Timestamp, now
	if err := vt.sink(&out); err != nil && !errors.Is(err, io.ErrClosedPipe) {
		slog.Debug("sfu layer write", "viewer", vt.viewer, "err", err)
	}
	return switched, ask, eff
}

// switchLocked makes idx the layer being forwarded, starting at the keyframe
// packet src: the numbers of the stream the viewer sees carry on from the last
// ones sent.
func (vt *viewerTrack) switchLocked(idx int, src *rtp.Packet, now time.Time) {
	if vt.started {
		vt.seqOff = vt.lastSeq + 1 - src.SequenceNumber
		ticks := uint32(now.Sub(vt.lastAt).Seconds() * videoClockRate)
		if ticks < 1 {
			ticks = 1
		}
		vt.tsOff = vt.lastTs + ticks - src.Timestamp
		vt.picOff, vt.tl0Off = 0, 0
		if vt.group.kind == codecVP8 {
			if d, ok := parseVP8(src.Payload); ok {
				if d.picPos >= 0 {
					vt.picOff = vt.lastPic + 1 - d.pic
				}
				if d.tl0Pos >= 0 {
					vt.tl0Off = vt.lastTl0 + 1 - d.tl0
				}
			}
		}
	}
	vt.started = true
	vt.cur = idx
	vt.curA.Store(int32(idx))
}
