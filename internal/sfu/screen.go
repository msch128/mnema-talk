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
	userID := peer.ID
	if layer > 0 {
		trackID += "#" + strconv.Itoa(layer)
	}
	key := trackKey(userID, trackID)
	info := &TrackInfo{
		SenderID: userID, Kind: webrtc.RTPCodecTypeVideo, Source: SourceScreen,
		publisher: peer, ssrc: ssrc, layer: layer,
	}

	r.mu.Lock()
	if r.peers[userID] != peer {
		r.mu.Unlock()
		drain(remote)
		return
	}
	g := r.groups[userID]
	if g == nil || g.superseded.Load() {
		id := fmt.Sprintf("%s~%d", trackKey(userID, trackID), layerGroupSeq.Add(1))
		g = newLayerGroup(id, r.ID, userID, r.notifier, codec)
		g.declared.Store(int32(len(r.layerMeta[userID])))
		if r.groups == nil {
			r.groups = make(map[uuid.UUID]*layerGroup)
		}
		r.groups[userID] = g
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
		slog.Warn("sfu screen layer refused", "user", userID, "layer", layer, "codec", codec.MimeType, "group", g.codec.MimeType)
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
