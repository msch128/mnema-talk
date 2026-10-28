package sfu

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
)

// A 4K screen share at 60 fps at the client's bitrate cap (12 Mbit/s), with a
// keyframe of about 1.5 MB every two seconds, never reaches the limit.
func TestLegitimate4KScreenShareIsNeverLimited(t *testing.T) {
	b := newIngressBucket(webrtc.RTPCodecTypeVideo)
	now := time.Now()
	const pkt = 1200
	const perSecond = 12_000_000 / 8 / pkt // 1250 packets
	for sec := range 60 {
		keyframe := sec%2 == 0
		for i := range perSecond {
			now = now.Add(time.Second / perSecond)
			if v := b.admit(pkt, now); v != ingressOK {
				t.Fatalf("packet %d of second %d limited (%d)", i, sec, v)
			}
			if keyframe && i == 0 {
				// The keyframe arrives within a few milliseconds.
				for range 1250 {
					now = now.Add(2 * time.Microsecond)
					if v := b.admit(pkt, now); v != ingressOK {
						t.Fatalf("keyframe packet of second %d limited", sec)
					}
				}
			}
		}
	}
}

func TestLegitimateAudioIsNeverLimited(t *testing.T) {
	b := newIngressBucket(webrtc.RTPCodecTypeAudio)
	now := time.Now()
	for i := range 50 * 120 { // two minutes of 20 ms Opus frames of 250 bytes
		now = now.Add(20 * time.Millisecond)
		if v := b.admit(250, now); v != ingressOK {
			t.Fatalf("audio packet %d limited", i)
		}
	}
}

func TestFloodIsDroppedThenTheConnectionIsClosed(t *testing.T) {
	b := newIngressBucket(webrtc.RTPCodecTypeAudio)
	now := time.Now()
	dropped, abusive := 0, 0
	// 5000 packets per second for 15 seconds.
	for range 15 * 5000 {
		now = now.Add(time.Second / 5000)
		switch b.admit(200, now) {
		case ingressDrop:
			dropped++
		case ingressAbusive:
			abusive++
		}
	}
	if dropped == 0 || abusive == 0 {
		t.Fatalf("dropped %d, abusive %d", dropped, abusive)
	}
	// A short burst over the limit is only dropped, and a clean second ends
	// the streak.
	b = newIngressBucket(webrtc.RTPCodecTypeVideo)
	now = time.Now()
	for range 6000 {
		if v := b.admit(1200, now); v == ingressAbusive {
			t.Fatal("a burst counted as abuse")
		}
	}
	now = now.Add(1500 * time.Millisecond)
	b.admit(100, now)
	now = now.Add(1500 * time.Millisecond)
	b.admit(100, now)
	if b.badWindows != 0 {
		t.Fatalf("badWindows = %d after a clean second", b.badWindows)
	}
}

func TestForwardDropsPacketsOverTheIngressLimit(t *testing.T) {
	user := uuid.New()
	r := &Room{peers: map[uuid.UUID]*Peer{}, trackLocals: map[string]*TrackInfo{}, notifier: &mediaNotifier{}}
	info := &TrackInfo{SenderID: user, Kind: webrtc.RTPCodecTypeAudio, Source: SourceAudio, publisher: &Peer{ID: user}}
	local := &failingLocal{err: nil}
	r.forward(&fakeRemote{n: 3000}, local, trackKey(user, "audio"), info)
	if local.writes < int(audioIngress.burstPkts)-5 || local.writes > int(audioIngress.burstPkts)+50 {
		t.Fatalf("forwarded %d of 3000 instantly arriving packets, want about %v", local.writes, audioIngress.burstPkts)
	}
}

func TestIngressAbuseClosesThePublisherConnection(t *testing.T) {
	s := newTestSFU(t)
	_, peer, err := s.Join(uuid.New(), uuid.New(), func(webrtc.SessionDescription) {}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ingressAbuse(&TrackInfo{SenderID: peer.ID, Source: SourceAudio, publisher: peer})
	if peer.PC.ConnectionState() != webrtc.PeerConnectionStateClosed {
		t.Fatalf("state = %v, want closed", peer.PC.ConnectionState())
	}
	ingressAbuse(&TrackInfo{SenderID: uuid.New(), Source: SourceAudio}) // no publisher: nothing to close
}

func TestForwardClosesAGrosslyAbusivePublisher(t *testing.T) {
	prev := ingressAbuseWindows
	ingressAbuseWindows = 0 // the first drop already counts
	t.Cleanup(func() { ingressAbuseWindows = prev })
	s := newTestSFU(t)
	_, peer, err := s.Join(uuid.New(), uuid.New(), func(webrtc.SessionDescription) {}, nil)
	if err != nil {
		t.Fatal(err)
	}
	r := &Room{peers: map[uuid.UUID]*Peer{}, trackLocals: map[string]*TrackInfo{}, notifier: &mediaNotifier{}}
	info := &TrackInfo{SenderID: peer.ID, Kind: webrtc.RTPCodecTypeAudio, Source: SourceAudio, publisher: peer}
	r.forward(&fakeRemote{n: 400}, &failingLocal{}, trackKey(peer.ID, "audio"), info)
	if peer.PC.ConnectionState() != webrtc.PeerConnectionStateClosed {
		t.Fatalf("state = %v, want closed", peer.PC.ConnectionState())
	}
}

func TestPeerHealthy(t *testing.T) {
	var none *Peer
	if none.Healthy() || (&Peer{}).Healthy() {
		t.Fatal("a peer without a connection is healthy")
	}
}

func TestJoinFailsWhenConnectionCannotBeBuilt(t *testing.T) {
	s := newTestSFU(t)
	s.iceServers = []webrtc.ICEServer{{URLs: []string{"turn:turn.invalid"}}} // a relay needs credentials
	if _, _, err := s.Join(uuid.New(), uuid.New(), func(webrtc.SessionDescription) {}, nil); err == nil {
		t.Fatal("join with an unusable ICE server succeeded")
	}
	r := &Room{peers: map[uuid.UUID]*Peer{}}
	if _, err := s.joinPeerIn(r, uuid.New(), nil, nil); err == nil {
		t.Fatal("room join with an unusable ICE server succeeded")
	}
}

func TestJoinFailsWhenTheSFUClosesWhileBuilding(t *testing.T) {
	s := newTestSFU(t)
	s.afterBuild = func() { s.closed.Store(true) }
	if _, _, err := s.Join(uuid.New(), uuid.New(), func(webrtc.SessionDescription) {}, nil); !errors.Is(err, errSFUClosed) {
		t.Fatalf("err = %v, want closed", err)
	}
	s.closed.Store(false)
}
