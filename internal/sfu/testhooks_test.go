package sfu

import (
	"testing"

	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
)

func TestPublishTestVideoRejectsUnsupportedSourceAndMissingPeer(t *testing.T) {
	s := newTestSFU(t)
	r, peer, err := s.Join(uuid.New(), uuid.New(), "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.PublishTestVideo(peer.ID, SourceAudio); err == nil {
		t.Fatal("audio accepted as video")
	}
	if err := r.PublishTestVideo(uuid.New(), SourceScreen); err == nil {
		t.Fatal("unjoined publisher accepted")
	}
	var subs Subscriptions
	if subs.Wants(peer.ID, webrtc.RTPCodecTypeVideo, SourceAudio) {
		t.Fatal("unknown video source forwarded")
	}
}
