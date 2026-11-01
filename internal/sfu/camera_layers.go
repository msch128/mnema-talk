package sfu

import (
	"slices"

	"github.com/google/uuid"
)

// A camera can go out as layers like a screen share (see layerGroup), but only
// when its sender says so first (SetCameraLayers), before its video starts.
// Without that, as for every client that does not know layers, a camera is
// forwarded as one plain track exactly as before, and the server never
// requires layers. A viewer is moved between the layers of a camera by what
// its picture of it needs (SetCameraFit); there is no picker.

// SetCameraLayers records the layers publisher's camera consists of, as the
// sender declares them (best first). It must come before the camera's first
// video; a camera that started without layers stays a single track.
func (r *Room) SetCameraLayers(publisher uuid.UUID, layers []Layer) error {
	if !validLayers(layers) {
		return ErrInvalidLayers
	}
	r.mu.Lock()
	if r.peers[publisher] == nil {
		r.mu.Unlock()
		return ErrNotInRoom
	}
	meta := r.layerMetaLocked(SourceCamera)
	if slices.Equal(meta[publisher], layers) {
		r.mu.Unlock()
		return nil
	}
	meta[publisher] = slices.Clone(layers)
	g := r.cameraGroups[publisher]
	r.mu.Unlock()
	if g != nil {
		g.setDeclared(len(layers))
	}
	return nil
}

// cameraLayeredLocked reports whether publisher declared more than one layer
// of its camera. r.mu is held.
func (r *Room) cameraLayeredLocked(publisher uuid.UUID) bool {
	return len(r.cameraLayerMeta[publisher]) > 1
}

// maxFitPixels bounds the picture height a viewer may report.
const maxFitPixels = maxLayerDimension

// MaxFitPixels is the tallest picture a viewer may report (SetCameraFit).
const MaxFitPixels = maxFitPixels

// SetCameraFit tells the SFU how many device pixels tall viewer's picture of
// publisher's camera is, so it gets the smallest layer that has enough. A
// larger picture climbs again. 0 means unknown (the best layer).
func (r *Room) SetCameraFit(viewer, publisher uuid.UUID, pixels int) error {
	if pixels < 0 || pixels > maxFitPixels {
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
	r.subsLocked(viewer).setCameraFit(publisher, pixels)
	layer := fitLayerFor(r.cameraLayerMeta[publisher], pixels)
	g := r.cameraGroups[publisher]
	r.mu.Unlock()
	if g == nil {
		return nil
	}
	if vt := g.viewerFor(viewer); vt != nil {
		vt.setFit(layer)
	}
	return nil
}

// cameraFitLocked is the best layer of publisher's camera viewer's picture
// can use. r.mu is held.
func (r *Room) cameraFitLocked(viewer, publisher uuid.UUID) int {
	return fitLayerFor(r.cameraLayerMeta[publisher], r.subs[viewer].cameraFit(publisher))
}

// cameraStoppedLocked forgets what a stopped camera declared, so a restart
// declares afresh. r.mu is held.
func (r *Room) cameraStoppedLocked(publisher uuid.UUID) {
	delete(r.cameraLayerMeta, publisher)
	if g := r.cameraGroups[publisher]; g != nil {
		g.shareStopped()
	}
}
