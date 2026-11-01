package sfu

import (
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
)

// layerGroupSeq makes every group's track ID unique, so a viewer's sender for
// a share that ended is never mistaken for the track of the next one.
var layerGroupSeq atomic.Uint64

// receiveScreenLayer starts forwarding one layer of peer's screen share. The
// first layer to arrive makes the group; the others join it.
func (r *Room) receiveScreenLayer(peer *Peer, remote rtpReader, trackID string, ssrc uint32, codec webrtc.RTPCodecCapability, layer int) {
	r.receiveLayer(peer, remote, SourceScreen, trackID, ssrc, codec, layer)
}

// groupsLocked is the layer groups of source's publishers. r.mu is held.
func (r *Room) groupsLocked(source Source) map[uuid.UUID]*layerGroup {
	if source == SourceCamera {
		if r.cameraGroups == nil {
			r.cameraGroups = make(map[uuid.UUID]*layerGroup)
		}
		return r.cameraGroups
	}
	if r.groups == nil {
		r.groups = make(map[uuid.UUID]*layerGroup)
	}
	return r.groups
}

// layerMetaLocked is what publishers declared about the layers of source.
func (r *Room) layerMetaLocked(source Source) map[uuid.UUID][]Layer {
	if source == SourceCamera {
		if r.cameraLayerMeta == nil {
			r.cameraLayerMeta = make(map[uuid.UUID][]Layer)
		}
		return r.cameraLayerMeta
	}
	if r.layerMeta == nil {
		r.layerMeta = make(map[uuid.UUID][]Layer)
	}
	return r.layerMeta
}

// groupFor is the layer group of publisher's source, or nil.
func (r *Room) groupFor(publisher uuid.UUID, source Source) *layerGroup {
	if source == SourceCamera {
		return r.cameraGroups[publisher]
	}
	return r.groups[publisher]
}

// receiveLayer starts forwarding one layer of peer's screen share or camera.
// The first layer to arrive makes the group; the others join it.
func (r *Room) receiveLayer(peer *Peer, remote rtpReader, source Source, trackID string, ssrc uint32, codec webrtc.RTPCodecCapability, layer int) {
	userID := peer.ID
	if layer > 0 {
		trackID += "#" + strconv.Itoa(layer)
	}
	key := trackKey(userID, trackID)
	info := &TrackInfo{
		SenderID: userID, Kind: webrtc.RTPCodecTypeVideo, Source: source,
		publisher: peer, ssrc: ssrc, layer: layer,
	}

	r.mu.Lock()
	if r.peers[userID] != peer {
		r.mu.Unlock()
		drain(remote)
		return
	}
	g := r.groupFor(userID, source)
	if g == nil && layer > 0 && source == SourceCamera && !r.cameraLayeredLocked(userID) {
		// A lower layer of a camera joins a group or a declaration; with
		// neither, that camera is sent as one plain track and has no layers.
		r.mu.Unlock()
		drain(remote)
		return
	}
	if g == nil || g.superseded.Load() {
		id := fmt.Sprintf("%s~%d", trackKey(userID, trackID), layerGroupSeq.Add(1))
		g = newLayerGroup(id, r.ID, userID, r.notifier, codec)
		g.source = source
		g.declared.Store(int32(len(r.layerMetaLocked(source)[userID])))
		r.groupsLocked(source)[userID] = g
	} else {
		// A new capture on a group that outlived the last share: its
		// wanted layers are announced afresh, whatever they were.
		g.mu.Lock()
		g.resetAnnouncedLocked()
		g.hold = [maxLayers]time.Time{}
		g.mu.Unlock()
	}
	r.mu.Unlock()

	info.group = g
	if !g.attach(info, codec) {
		slog.Warn("sfu layer refused", "user", userID, "source", string(source), "layer", layer, "codec", codec.MimeType, "group", g.codec.MimeType)
		drain(remote)
		return
	}
	r.addTrack(key, info)
	go r.forward(remote, layerWriter{group: g, layer: layer}, key, info)
}

// drain reads and discards a track nobody forwards, so its buffers stay empty.
func drain(remote rtpReader) {
	go func() {
		buf := make([]byte, 1500)
		for {
			if _, _, err := remote.Read(buf); err != nil {
				return
			}
		}
	}()
}

// SetScreenLayers records the layers publisher's screen share consists of, as
// the sender declares them (best first), and announces them to the room.
func (r *Room) SetScreenLayers(publisher uuid.UUID, layers []Layer) error {
	if !validLayers(layers) {
		return ErrInvalidLayers
	}
	r.mu.Lock()
	if r.peers[publisher] == nil {
		r.mu.Unlock()
		return ErrNotInRoom
	}
	if r.layerMeta == nil {
		r.layerMeta = make(map[uuid.UUID][]Layer)
	}
	if slices.Equal(r.layerMeta[publisher], layers) {
		r.mu.Unlock()
		return nil
	}
	r.layerMeta[publisher] = slices.Clone(layers)
	g := r.groups[publisher]
	r.mu.Unlock()
	if g != nil {
		g.setDeclared(len(layers))
	}
	// notifyMedia announces the change to the room.
	go r.SignalPeerConnections()
	return nil
}

// ScreenLayers returns the declared layers of every live screen share.
func (r *Room) ScreenLayers() map[uuid.UUID][]Layer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.screenLayersLocked(r.mediaStatesLocked())
}

func (r *Room) screenLayersLocked(states map[uuid.UUID]MediaState) map[uuid.UUID][]Layer {
	out := map[uuid.UUID][]Layer{}
	for sharer, st := range states {
		if layers := r.layerMeta[sharer]; st.Screen && len(layers) > 0 {
			out[sharer] = slices.Clone(layers)
		}
	}
	return out
}

// SetScreenQuality chooses which layer of publisher's screen share viewer
// receives: -1 leaves it to the SFU, otherwise the index of a layer (best
// first), clamped to the layers the share has.
func (r *Room) SetScreenQuality(viewer, publisher uuid.UUID, layer int) error {
	if layer < -1 || layer >= maxLayers {
		return ErrInvalidLayers
	}
	r.mu.Lock()
	if r.peers[viewer] == nil {
		r.mu.Unlock()
		return ErrNotInRoom
	}
	if viewer == publisher {
		r.mu.Unlock()
		return ErrInvalidLayers
	}
	r.subsLocked(viewer).setScreenQuality(publisher, layer)
	g := r.groups[publisher]
	r.mu.Unlock()
	if g == nil {
		return nil
	}
	if vt := g.viewerFor(viewer); vt != nil {
		vt.setPref(layer)
	}
	return nil
}

// SetScreenFit tells the SFU the best layer of publisher's screen share that
// viewer's picture can use (0 is the best the share has, larger is smaller).
// A viewer on automatic never gets a better layer than that, so a small tile
// costs a small stream, and climbs when the picture grows.
func (r *Room) SetScreenFit(viewer, publisher uuid.UUID, layer int) error {
	if layer < 0 || layer >= maxLayers {
		return ErrInvalidLayers
	}
	r.mu.Lock()
	if r.peers[viewer] == nil {
		r.mu.Unlock()
		return ErrNotInRoom
	}
	if viewer == publisher {
		r.mu.Unlock()
		return ErrInvalidLayers
	}
	r.subsLocked(viewer).setScreenFit(publisher, layer)
	g := r.groups[publisher]
	r.mu.Unlock()
	if g == nil {
		return nil
	}
	if vt := g.viewerFor(viewer); vt != nil {
		vt.setFit(layer)
	}
	return nil
}
