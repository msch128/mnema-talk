package sfu

import (
	"testing"

	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
)

func TestStatsCountsRoomsPeersAndVideo(t *testing.T) {
	alice, bob, carol := uuid.New(), uuid.New(), uuid.New()
	busy := &Room{
		peers: map[uuid.UUID]*Peer{alice: {}, bob: {}},
		trackLocals: map[string]*TrackInfo{
			"a-screen": {SenderID: alice, Kind: webrtc.RTPCodecTypeVideo, Source: SourceScreen},
			"a-cam":    {SenderID: alice, Kind: webrtc.RTPCodecTypeVideo, Source: SourceCamera},
			"b-mic":    {SenderID: bob, Kind: webrtc.RTPCodecTypeAudio},
		},
	}
	quiet := &Room{peers: map[uuid.UUID]*Peer{carol: {}}, trackLocals: map[string]*TrackInfo{}}
	empty := &Room{peers: map[uuid.UUID]*Peer{}, trackLocals: map[string]*TrackInfo{}}
	s := &SFU{rooms: map[uuid.UUID]*Room{uuid.New(): busy, uuid.New(): quiet, uuid.New(): empty}}

	got := s.Stats()
	want := Stats{Rooms: 2, Peers: 3, ScreenShares: 1, Cameras: 1}
	if got != want {
		t.Fatalf("Stats() = %+v, want %+v", got, want)
	}
}
