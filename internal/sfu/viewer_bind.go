package sfu

import "github.com/pion/webrtc/v4"

// expectBind makes the track wait for its connection to bind it. Pion's
// WriteRTP succeeds without error while nothing is bound, so a keyframe sent
// before the browser's answer would be counted as delivered and the viewer
// would sit on undecodable delta frames (a picture that stays 0x0).
func (vt *viewerTrack) expectBind() {
	vt.mu.Lock()
	vt.unbound = true
	vt.mu.Unlock()
}

// Bind is Pion's call when the viewer's answer arrives. The keyframe asked for
// when the viewer was added may have passed already, so ask again.
func (vt *viewerTrack) Bind(t webrtc.TrackLocalContext) (webrtc.RTPCodecParameters, error) {
	params, err := vt.TrackLocalStaticRTP.Bind(t)
	if err != nil {
		return params, err
	}
	vt.bound()
	return params, nil
}

func (vt *viewerTrack) bound() {
	vt.mu.Lock()
	vt.unbound = false
	vt.lastKeyAsk = time.Time{}
	vt.mu.Unlock()
	vt.retarget()
}
