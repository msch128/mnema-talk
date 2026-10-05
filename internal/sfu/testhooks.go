package sfu

import (
	"errors"

	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
)

// PublishTestVideo registers a screen or camera track for sender, who must
// have a peer in the room, as if its first packets had arrived, and signals
// the room. No media flows. It lets packages built on the SFU test what
// happens around a live share without a real WebRTC connection; the server
// never calls it.
func (r *Room) PublishTestVideo(sender uuid.UUID, source Source) error {
	if source != SourceScreen && source != SourceCamera {
		return errors.New("not a video source")
	}
	r.mu.RLock()
	peer := r.peers[sender]
	r.mu.RUnlock()
	if peer == nil {
		return errors.New("sender has no peer in the room")
	}
	key := trackKey(sender, "test-"+string(source))
	streamID := sender.String()
	if source == SourceCamera {
		streamID = CameraStreamPrefix + streamID
	}
	local, err := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8}, key, streamID)
	if err != nil {
		return err
	}
	r.addTrack(key, &TrackInfo{Track: local, SenderID: sender, Kind: webrtc.RTPCodecTypeVideo, Source: source, publisher: peer})
	return nil
}
