package main

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/gorilla/websocket"
	"github.com/msch128/mnema-talk/loadtest/viewer/internal/lt"
	"github.com/pion/webrtc/v4"
)

// dialVoice opens the app's WebSocket with the member's session cookie.
func dialVoice(base *url.URL, tk token) (*websocket.Conn, error) {
	scheme, cookie := "ws", lt.CookieName(base)
	if base.Scheme == "https" {
		scheme = "wss"
	}
	h := http.Header{}
	h.Set("Origin", lt.Origin(base))
	h.Set("Cookie", cookie+"="+tk.Token)
	conn, resp, err := websocket.DefaultDialer.Dial(scheme+"://"+base.Host+"/api/ws", h)
	if err != nil {
		if resp != nil {
			return nil, fmt.Errorf("dial: %w (%s)", err, resp.Status)
		}
		return nil, fmt.Errorf("dial: %w", err)
	}
	return conn, nil
}

// watchPeer records the peer connection's state changes on the member.
func (m *member) watchPeer(pc *webrtc.PeerConnection) {
	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c != nil {
			m.event("local candidate %s %s %s:%d", c.Protocol, c.Typ, c.Address, c.Port)
			m.sendCandidate(c.ToJSON())
		}
	})
	pc.OnICEGatheringStateChange(func(s webrtc.ICEGatheringState) { m.event("gathering %s", s) })
	pc.OnICEConnectionStateChange(func(s webrtc.ICEConnectionState) { m.event("ice %s", s) })
	pc.OnSignalingStateChange(func(s webrtc.SignalingState) { m.event("signaling %s", s) })
	pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		m.event("connection %s", s)
		ok := s == webrtc.PeerConnectionStateConnected
		if !ok && m.connected.Load() {
			m.flaps.Add(1)
		}
		if ok && !m.everConn.Swap(true) {
			m.connMs.Store(time.Since(m.startedAt).Milliseconds())
		}
		m.connected.Store(ok)
	})
}

// countTrack reads a remote track until it ends and counts what arrives.
func (m *member) countTrack(tr *webrtc.TrackRemote) {
	video := tr.Kind() == webrtc.RTPCodecTypeVideo
	if !video {
		m.audioT.Add(1)
	}
	stats := newTrackStats(classify(tr.Kind(), tr.StreamID()), freezeGap())
	m.addTrack(stats)
	buf := make([]byte, 1600)
	for {
		n, _, err := tr.Read(buf)
		if err != nil {
			return
		}
		if n >= 4 {
			stats.observe(time.Now(), n, binary.BigEndian.Uint16(buf[2:4]))
		}
		if video {
			m.videoMs.CompareAndSwap(0, time.Since(m.startedAt).Milliseconds()+1)
			m.videoB.Add(int64(n))
		} else {
			m.audioB.Add(int64(n))
		}
	}
}

// keepSubscribed renews the subscriptions every 5 s until the member stops:
// the screens it watches and the cameras it keeps visible.
func (m *member) keepSubscribed(sharers []string) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		for _, sharer := range sharers {
			if sharer == m.id {
				continue
			}
			sub := map[string]any{"kind": "screen", "user_id": sharer, "on": true}
			if q := m.quality(); q != nil {
				sub["quality"] = q
			}
			m.send("webrtc_subscribe", sub)
		}
		m.subscribeCameras()
		select {
		case <-t.C:
		case <-m.stop:
			return
		}
	}
}

// answerOffer applies the SFU's offer and sends the answer. A step that fails
// leaves the offer unanswered, like any lost message.
func (m *member) answerOffer(pc *webrtc.PeerConnection, payload json.RawMessage) {
	m.offers.Add(1)
	m.event("offer %d received", m.offers.Load())
	var offer webrtc.SessionDescription
	if json.Unmarshal(payload, &offer) != nil {
		return
	}
	if err := pc.SetRemoteDescription(offer); err != nil {
		return
	}
	if err := m.startPublishing(pc); err != nil {
		m.failed.CompareAndSwap(nil, err.Error())
		return
	}
	m.remoteApplied()
	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		return
	}
	if err := pc.SetLocalDescription(answer); err != nil {
		return
	}
	m.send("webrtc_answer", pc.LocalDescription())
	m.event("answer sent")
	m.answerWasSent()
}

// readLoop handles the server's messages until the connection fails or the
// member is kicked.
func (m *member) readLoop(conn *websocket.Conn, pc *webrtc.PeerConnection) error {
	for {
		var ev struct {
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := conn.ReadJSON(&ev); err != nil {
			return fmt.Errorf("read: %w", err)
		}
		switch ev.Type {
		case "webrtc_offer":
			m.answerOffer(pc, ev.Payload)
		case "webrtc_candidate":
			var c webrtc.ICECandidateInit
			if json.Unmarshal(ev.Payload, &c) == nil {
				m.addRemote(c)
			}
		case "voice_kicked":
			return fmt.Errorf("kicked: %s", ev.Payload)
		}
	}
}

func run(api *webrtc.API, c viewerConfig, tk token, m *member) error {
	conn, err := dialVoice(c.base, tk)
	if err != nil {
		return err
	}
	m.conn = conn
	rtcConfig, err := c.rtcConfiguration(tk)
	if err != nil {
		return err
	}
	pc, err := api.NewPeerConnection(rtcConfig)
	if err != nil {
		return err
	}
	m.pc = pc
	m.watchPeer(pc)
	pc.OnTrack(func(tr *webrtc.TrackRemote, _ *webrtc.RTPReceiver) { m.countTrack(tr) })
	if os.Getenv("LEGACY_SIGNALLING") == "1" {
		m.candMu.Lock()
		m.remoteSet, m.answerSent = true, true
		m.candMu.Unlock()
	}
	m.startedAt = time.Now()
	m.send("voice_join", map[string]any{"channel_id": c.channel})
	if d := iceTimeout(); d > 0 {
		go m.watchConnect(d)
	}
	m.send("voice_mute_state", map[string]any{"muted": !m.role.audio, "deafened": false})
	go m.keepSubscribed(c.sharers)
	return m.readLoop(conn, pc)
}

// cameraGap spaces the camera opt-outs: the server takes 40 events a second
// per connection.
const cameraGap = 30 * time.Millisecond

// subscribeCameras sends the member's camera choice: every camera is on unless
// the plan hides all of them or some of them.
func (m *member) subscribeCameras() {
	m.send("webrtc_subscribe", map[string]any{"kind": "camera", "all": true, "on": !m.cameras.allOff})
	for _, id := range m.cameras.hide {
		m.send("webrtc_subscribe", map[string]any{"kind": "camera", "user_id": id, "on": false})
		time.Sleep(cameraGap)
	}
}
