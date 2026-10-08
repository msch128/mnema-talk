//go:build integration

// Separate ignored overlay owned by desktop_ui_quality. Real native TLS-WSS,
// DB sessions and Pion audio RTP; public marker bytes, no E2EE/provider claim.
package ws

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/sfu"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

type mediaProbePacket struct {
	ssrc     uint32
	sequence uint16
}

type mediaProbeEndpoint struct {
	pc                         *webrtc.PeerConnection
	conn                       *websocket.Conn
	outgoing                   *webrtc.TrackLocalStaticRTP
	videoOutgoing              *webrtc.TrackLocalStaticRTP
	videoSequence              uint16
	videoTimestamp             uint32
	packetCounts               map[mediaProbePacket]int
	lastVideo                  map[byte]mediaProbePacket
	writeMu, rtpMu, receivedMu sync.Mutex
	received                   map[byte]int
	sequence                   uint16
	timestamp                  uint32
	ended                      chan struct{}
}

func (e *mediaProbeEndpoint) write(kind string, payload any) error {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()
	_ = e.conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	return e.conn.WriteJSON(map[string]any{"type": kind, "payload": payload})
}
func (e *mediaProbeEndpoint) inject(marker byte) {
	e.rtpMu.Lock()
	defer e.rtpMu.Unlock()
	e.sequence++
	e.timestamp += 960
	_ = e.outgoing.WriteRTP(&rtp.Packet{Header: rtp.Header{Version: 2, SequenceNumber: e.sequence, Timestamp: e.timestamp, SSRC: 77}, Payload: []byte{0x78, marker, 0x55, 0x99, 0x66, 0x88, 0x33, 0x11}})
}
func (e *mediaProbeEndpoint) injectVideo(marker byte) {
	e.rtpMu.Lock()
	defer e.rtpMu.Unlock()
	e.videoSequence++
	e.videoTimestamp += 3000
	_ = e.videoOutgoing.WriteRTP(&rtp.Packet{Header: rtp.Header{Version: 2, SequenceNumber: e.videoSequence, Timestamp: e.videoTimestamp, SSRC: 78, Marker: true}, Payload: []byte{0x10, marker, 1, 3, 5, 7, 9, 11}})
}
func (e *mediaProbeEndpoint) videoPacket(marker byte) (mediaProbePacket, int) {
	e.receivedMu.Lock()
	defer e.receivedMu.Unlock()
	p := e.lastVideo[marker]
	return p, e.packetCounts[p]
}
func (e *mediaProbeEndpoint) packetCount(p mediaProbePacket) int {
	e.receivedMu.Lock()
	defer e.receivedMu.Unlock()
	return e.packetCounts[p]
}
func (e *mediaProbeEndpoint) nack(p mediaProbePacket) error {
	return e.pc.WriteRTCP([]rtcp.Packet{&rtcp.TransportLayerNack{SenderSSRC: 1234, MediaSSRC: p.ssrc, Nacks: []rtcp.NackPair{{PacketID: p.sequence}}}})
}
func (e *mediaProbeEndpoint) count(marker byte) int {
	e.receivedMu.Lock()
	defer e.receivedMu.Unlock()
	return e.received[marker]
}
func mediaProbeAttach(t *testing.T, conn *websocket.Conn, channel uuid.UUID, publish bool) *mediaProbeEndpoint {
	t.Helper()
	setting := webrtc.SettingEngine{}
	setting.SetIncludeLoopbackCandidate(true)
	setting.SetIPFilter(func(ip net.IP) bool { return ip.IsLoopback() })
	api := webrtc.NewAPI(webrtc.WithSettingEngine(setting))
	pc, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal("media fixture peer unavailable")
	}
	e := &mediaProbeEndpoint{pc: pc, conn: conn, received: make(map[byte]int), packetCounts: make(map[mediaProbePacket]int), lastVideo: make(map[byte]mediaProbePacket), ended: make(chan struct{})}
	t.Cleanup(func() { _ = pc.Close(); _ = conn.Close() })
	if publish {
		e.outgoing, err = webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 2}, "synthetic-public-marker", "synthetic")
		if err != nil {
			t.Fatal("fixture track unavailable")
		}
		if _, err = pc.AddTrack(e.outgoing); err != nil {
			t.Fatal("fixture track attach failed")
		}
		e.videoOutgoing, err = webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8, ClockRate: 90000}, "synthetic-video-marker", "synthetic")
		if err != nil {
			t.Fatal("fixture video track unavailable")
		}
		if _, err = pc.AddTrack(e.videoOutgoing); err != nil {
			t.Fatal("fixture video attach failed")
		}
	}
	pc.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		go func() {
			for {
				// TrackRemote.Read checks queued RTX before waiting for the next
				// original RTP packet. Bounded video reads also drain actual RTX when
				// no original video is emitted after a cutoff/NACK request.
				if track.Kind() == webrtc.RTPCodecTypeVideo {
					_ = track.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
				}
				packet, _, err := track.ReadRTP()
				if err != nil {
					var timeout net.Error
					if errors.As(err, &timeout) && timeout.Timeout() {
						continue
					}
					return
				}
				if len(packet.Payload) > 1 {
					e.receivedMu.Lock()
					e.received[packet.Payload[1]]++
					if track.Kind() == webrtc.RTPCodecTypeVideo {
						key := mediaProbePacket{ssrc: packet.SSRC, sequence: packet.SequenceNumber}
						e.packetCounts[key]++
						e.lastVideo[packet.Payload[1]] = key
					}
					e.receivedMu.Unlock()
				}
			}
		}()
	})
	_ = conn.SetReadDeadline(time.Time{})
	go func() {
		defer close(e.ended)
		pending := []webrtc.ICECandidateInit{}
		for {
			var event struct {
				Type    string          `json:"type"`
				Payload json.RawMessage `json:"payload"`
			}
			if conn.ReadJSON(&event) != nil {
				return
			}
			switch event.Type {
			case "webrtc_candidate":
				var candidate webrtc.ICECandidateInit
				if json.Unmarshal(event.Payload, &candidate) != nil {
					return
				}
				if pc.RemoteDescription() == nil {
					pending = append(pending, candidate)
				} else {
					_ = pc.AddICECandidate(candidate)
				}
			case "webrtc_offer":
				var offer webrtc.SessionDescription
				if json.Unmarshal(event.Payload, &offer) != nil || pc.SetRemoteDescription(offer) != nil {
					return
				}
				for _, candidate := range pending {
					_ = pc.AddICECandidate(candidate)
				}
				pending = nil
				answer, err := pc.CreateAnswer(nil)
				if err != nil {
					return
				}
				gathered := webrtc.GatheringCompletePromise(pc)
				if pc.SetLocalDescription(answer) != nil {
					return
				}
				select {
				case <-gathered:
				case <-time.After(3 * time.Second):
					return
				}
				if e.write("webrtc_answer", pc.LocalDescription()) != nil {
					return
				}
			}
		}
	}()
	if e.write("voice_join", map[string]any{"channel_id": channel}) != nil {
		t.Fatal("fixture real voice join failed")
	}
	return e
}
func mediaProbeUser(t *testing.T, a *nativeWSApp) auth.User {
	t.Helper()
	u := auth.User{ID: uuid.New(), Username: "media-" + uuid.NewString()[:18], Role: auth.RoleUser}
	hash, err := auth.HashPassword(nativeFixturePassword)
	if err != nil {
		t.Fatal("fixture password hash failed")
	}
	if _, err = a.pool.Exec(context.Background(), `INSERT INTO users(id,username,display_name,password_hash,role)VALUES($1,$2,$2,$3,'user')`, u.ID, u.Username, hash); err != nil {
		t.Fatal("fixture user insert failed")
	}
	t.Cleanup(func() { _, _ = a.pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1`, u.ID) })
	return u
}
func mediaProbeGrant(t *testing.T, a *nativeWSApp, u auth.User) nativeWSGrant {
	previous := a.user
	a.user = u
	grant := a.login(t)
	a.user = previous
	return grant
}
func mediaProbeChannel(t *testing.T, a *nativeWSApp) uuid.UUID {
	id := uuid.New()
	if _, err := a.pool.Exec(context.Background(), `INSERT INTO channels(id,name,type)VALUES($1,'native-media-fixture','voice')`, id); err != nil {
		t.Fatal("fixture channel insert failed")
	}
	t.Cleanup(func() { _, _ = a.pool.Exec(context.Background(), `DELETE FROM channels WHERE id=$1`, id) })
	return id
}
func mediaProbeWait(t *testing.T, predicate func() bool, message string) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for !predicate() {
		if time.Now().After(deadline) {
			t.Fatal(message)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestNativeMediaProbeBulkFamilyCutoffWhileBothVoiceLocksHeld(t *testing.T) {
	relay, err := sfu.NewSFU(0, 0, []string{"127.0.0.1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = relay.Close() })
	a := newNativeWSApp(t, relay, nil)
	firstChannel, secondChannel := mediaProbeChannel(t, a), mediaProbeChannel(t, a)
	revoked := a.login(t)
	badPubConn, badRecvConn := a.dial(t, revoked), a.dial(t, revoked)
	readNativeEvent(t, badPubConn, "voice_rooms")
	readNativeEvent(t, badRecvConn, "voice_rooms")
	goodUser := mediaProbeUser(t, a)
	goodGrant := mediaProbeGrant(t, a, goodUser)
	goodConn := a.dial(t, goodGrant)
	readNativeEvent(t, goodConn, "voice_rooms")
	otherUser := mediaProbeUser(t, a)
	otherGrant := mediaProbeGrant(t, a, otherUser)
	otherConn := a.dial(t, otherGrant)
	readNativeEvent(t, otherConn, "voice_rooms")
	browserUser := mediaProbeUser(t, a)
	cookie := httptest.NewRecorder()
	if a.accounts.Sessions.Start(cookie, &browserUser, 0) != nil {
		t.Fatal("browser fixture session failed")
	}
	browserConn, _, err := a.dialer.Dial("wss"+strings.TrimPrefix(a.server.URL, "https")+"/api/ws", http.Header{"Origin": []string{a.server.URL}, "Cookie": []string{cookie.Result().Cookies()[0].String()}})
	if err != nil {
		t.Fatal("browser fixture WSS failed")
	}
	readNativeEvent(t, browserConn, "voice_rooms")
	badPublisher := mediaProbeAttach(t, badPubConn, firstChannel, true)
	badReceiver := mediaProbeAttach(t, badRecvConn, secondChannel, false)
	good := mediaProbeAttach(t, goodConn, firstChannel, true)
	other := mediaProbeAttach(t, otherConn, secondChannel, true)
	browser := mediaProbeAttach(t, browserConn, firstChannel, false)
	// Before-cutoff markers establish both real directions and control families.
	mediaProbeWait(t, func() bool {
		badPublisher.inject(0x10)
		good.inject(0x20)
		other.inject(0x30)
		return good.count(0x10) > 0 && browser.count(0x10) > 0 && browser.count(0x20) > 0 && badReceiver.count(0x30) > 0
	}, "initial real Pion audio forwarding did not flow")
	// Default Pion NACK feedback is negotiated for VP8, not Opus. Use real
	// screen subscriptions and exact received SSRC/sequence identities.
	if browser.write("webrtc_subscribe", map[string]any{"kind": "screen", "user_id": goodUser.ID, "on": true}) != nil || badReceiver.write("webrtc_subscribe", map[string]any{"kind": "screen", "user_id": otherUser.ID, "on": true}) != nil || browser.write("webrtc_subscribe", map[string]any{"kind": "screen", "user_id": a.user.ID, "on": true}) != nil || good.write("webrtc_subscribe", map[string]any{"kind": "screen", "user_id": a.user.ID, "on": true}) != nil {
		t.Fatal("actual video subscription failed")
	}
	mediaProbeWait(t, func() bool {
		good.injectVideo(0x40)
		other.injectVideo(0x50)
		badPublisher.injectVideo(0x60)
		_, bc := browser.videoPacket(0x40)
		_, rc := badReceiver.videoPacket(0x50)
		_, bs := browser.videoPacket(0x60)
		_, gs := good.videoPacket(0x60)
		return bc > 0 && rc > 0 && bs > 0 && gs > 0
	}, "actual VP8 cached-packet setup did not flow")
	browserPacket, _ := browser.videoPacket(0x40)
	revokedPacket, _ := badReceiver.videoPacket(0x50)
	browserSourcePacket, _ := browser.videoPacket(0x60)
	goodSourcePacket, _ := good.videoPacket(0x60)
	browserBefore, revokedBefore := browser.packetCount(browserPacket), badReceiver.packetCount(revokedPacket)
	browserSourceBefore, goodSourceBefore := browser.packetCount(browserSourcePacket), good.packetCount(goodSourcePacket)
	if browser.nack(browserPacket) != nil || badReceiver.nack(revokedPacket) != nil || browser.nack(browserSourcePacket) != nil || good.nack(goodSourcePacket) != nil {
		t.Fatal("actual pre-seal RTCP NACK write failed")
	}
	mediaProbeWait(t, func() bool {
		return browser.packetCount(browserPacket) > browserBefore && badReceiver.packetCount(revokedPacket) > revokedBefore && browser.packetCount(browserSourcePacket) > browserSourceBefore && good.packetCount(goodSourcePacket) > goodSourceBefore
	}, "positive real cached NACK retransmission absent before seal")
	// Pion caches one RTX sequence per original packet. Asking for that same
	// cached packet a second time can be discarded by SRTP replay protection.
	// Establish DISTINCT cached originals for post-seal first-RTX requests;
	// do not weaken SRTP or confuse replay filtering with authorization cutoff.
	mediaProbeWait(t, func() bool {
		good.injectVideo(0x41)
		other.injectVideo(0x51)
		badPublisher.injectVideo(0x61)
		_, bc := browser.videoPacket(0x41)
		_, rc := badReceiver.videoPacket(0x51)
		_, bs := browser.videoPacket(0x61)
		_, gs := good.videoPacket(0x61)
		return bc == 1 && rc == 1 && bs == 1 && gs == 1
	}, "distinct first-RTX cached originals did not flow before seal")
	browserPacket, _ = browser.videoPacket(0x41)
	revokedPacket, _ = badReceiver.videoPacket(0x51)
	browserSourcePacket, _ = browser.videoPacket(0x61)
	goodSourcePacket, _ = good.videoPacket(0x61)
	// No additional original video packets are emitted after retaining these
	// exact cached packets. Audio controls remain independently observable.
	var selected []*Client
	a.hub.mu.RLock()
	for c := range a.hub.clients {
		if c.native != nil && c.native.binding.FamilyID() == revoked.Family {
			selected = append(selected, c)
		}
	}
	a.hub.mu.RUnlock()
	if len(selected) != 2 {
		t.Fatal("expected both SAME-family native sockets")
	}
	for _, c := range selected {
		c.voiceMu.Lock()
	}
	var unlockOnce sync.Once
	unlock := func() {
		unlockOnce.Do(func() {
			for _, c := range selected {
				c.voiceMu.Unlock()
			}
		})
	}
	defer unlock()
	principal, err := a.sessions.AuthenticateAccess(context.Background(), revoked.Access)
	if err != nil {
		t.Fatal("genuine family principal failed")
	}
	done := make(chan error, 1)
	go func() { done <- a.sessions.RevokeFamily(context.Background(), principal) }()
	mediaProbeWait(t, func() bool {
		return selected[0].native.securityClosed.Load() && selected[1].native.securityClosed.Load()
	}, "bulk family seal waited on first voice lock")
	select {
	case <-done:
		t.Fatal("fixture locks did not hold physical voice cleanup")
	default:
	}
	// Deliberately KEEP remote Pion PCs alive after WSS closure. New marker bytes
	// are created only after BOTH security latches seal; pre-cutoff buffers cannot
	// contain them. Source/destination gates must stop these packets themselves.
	time.Sleep(100 * time.Millisecond) // disclose/drain pre-seal already-in-flight data.
	revokedCachedBefore, browserCachedBefore := badReceiver.packetCount(revokedPacket), browser.packetCount(browserPacket)
	browserSourceCachedBefore, goodSourceCachedBefore := browser.packetCount(browserSourcePacket), good.packetCount(goodSourcePacket)
	if badReceiver.nack(revokedPacket) != nil || browser.nack(browserPacket) != nil || browser.nack(browserSourcePacket) != nil || good.nack(goodSourcePacket) != nil {
		t.Fatal("actual post-seal RTCP NACK write failed")
	}
	time.Sleep(350 * time.Millisecond)
	if badReceiver.packetCount(revokedPacket) != revokedCachedBefore {
		t.Fatal("cached recipient NACK crossed seal before any original post-seal packet was injected")
	}
	if browser.packetCount(browserSourcePacket) != browserSourceCachedBefore || good.packetCount(goodSourcePacket) != goodSourceCachedBefore {
		t.Fatal("cached source NACK crossed seal before any original post-seal packet was injected")
	}
	if browser.packetCount(browserPacket) <= browserCachedBefore {
		t.Fatal("actual browser cached NACK control absent while original video was idle")
	}
	controlBefore := browser.count(0x21)
	until := time.Now().Add(700 * time.Millisecond)
	for time.Now().Before(until) {
		badPublisher.inject(0x11)
		other.inject(0x31)
		good.inject(0x21)
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(150 * time.Millisecond)
	if badReceiver.packetCount(revokedPacket) != revokedCachedBefore {
		t.Fatal("cached RTCP NACK retransmission crossed revoked recipient gate while voiceMu held")
	}
	if browser.packetCount(browserSourcePacket) != browserSourceCachedBefore || good.packetCount(goodSourcePacket) != goodSourceCachedBefore {
		t.Fatal("cached source RTCP NACK retransmission crossed revoked publisher gate while voiceMu held")
	}
	if browser.packetCount(browserPacket) <= browserCachedBefore {
		t.Fatal("browser cached RTCP NACK control stopped after unrelated family seal")
	}
	if good.count(0x11) != 0 || browser.count(0x11) != 0 {
		t.Fatal("new post-seal source RTP crossed native authorization while voiceMu held")
	}
	if badReceiver.count(0x31) != 0 {
		t.Fatal("new post-seal RTP reached revoked destination while voiceMu held")
	}
	if browser.count(0x21) <= controlBefore {
		t.Fatal("other-family native publisher/browser receiver stopped")
	}
	if !a.client(t, goodGrant.Family).nativeLive() || !a.client(t, otherGrant.Family).nativeLive() {
		t.Fatal("family isolation failed")
	}
	for _, c := range selected {
		if c.peer() == nil || c.peer().PC.ConnectionState() == webrtc.PeerConnectionStateClosed {
			t.Fatal("physical peer unexpectedly closed despite held cleanup mutex")
		}
	}
	unlock()
	select {
	case err = <-done:
		if err != nil {
			t.Fatal("committed family revoke failed")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("revoke cleanup did not finish after unlock")
	}
	mediaProbeWait(t, func() bool { return relay.Stats().Peers == 3 }, "exact revoked peers leaked after cleanup")
}
