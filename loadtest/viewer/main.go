// Command viewer is a lightweight Talk member for load tests: it joins a voice
// channel over the app's WebSocket, answers the SFU's offers with Pion,
// subscribes to one member's screen and counts what arrives. It holds no MLS
// keys, so it only ever sees ciphertext; real decrypting clients are the
// browser scenarios next to it.
//
// Configuration is read from the environment:
//
//	BASE_URL      instance URL (http:// or https://)
//	TOKENS        path of a JSON file [{"id": "...", "token": "..."}]
//	FROM, TO      slice of TOKENS to run (TO exclusive, default: all)
//	CHANNEL_ID    voice channel to join
//	SHARER_ID     user whose screen to subscribe to
//	HOLD_S        seconds to stay (default 60)
//	SPACING_MS    pause between two joins (default 300)
//	LOSS          fraction of incoming RTP packets dropped before NACK
//	              generation, 0..1 (default 0)
//	JITTER_MS     random per-packet delay up to this many ms (default 0)
//	BW_KBPS       limit of each viewer's link in kbit/s, with a queue of
//	              BW_QUEUE_MS (default 1500) before packets are dropped;
//	              0 = unlimited (default). A link too slow for the layer
//	              shows a rising delay first and loss only later
//	LEGACY_SIGNALLING=1  diagnostic: send candidates before the answer and
//	              apply remote ones without a remote description (the
//	              pre-0.8 scratch client), to reproduce ICE failures
//	QUALITY       layer of the screen share to watch: "auto" (default), a
//	              layer index 0..2 (0 is the best), or "spread" to give each
//	              viewer layer index%3, so a run exercises all layers
//	ICE_TIMEOUT_S a member that is not connected this long after its join is
//	              recorded as failed (with its ICE diagnostics) and closed
//	              (default 25, 0 = never)
//	CLOSE_TIMEOUT_S  bound for tearing the members down at the end (default 10);
//	              the result is written first, so a stuck Close cannot hide it
//	ICE_INTERFACES  comma separated interface names the viewer may gather
//	              candidates on (default: all), to compare multi-homed hosts
//	ICE_UDP4_ONLY=1  gather IPv4 UDP candidates only
//	STOP_FILE     optional path; the run ends early once the file exists
//	OUT           optional path of the JSON result
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v4"
)

// runBounded runs fns concurrently and returns after they all finished or
// limit passed, with the number that finished. Stuck ones are left running.
func runBounded(fns []func(), limit time.Duration) int {
	var done atomic.Int64
	var wg sync.WaitGroup
	for _, fn := range fns {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fn()
			done.Add(1)
		}()
	}
	finished := make(chan struct{})
	go func() { wg.Wait(); close(finished) }()
	select {
	case <-finished:
	case <-time.After(limit):
	}
	return int(done.Load())
}

func run(api *webrtc.API, base *url.URL, tk token, channel, sharer string, m *member) error {
	scheme, cookie := "ws", "mnema_session"
	if base.Scheme == "https" {
		scheme, cookie = "wss", "__Host-mnema_session"
	}
	h := http.Header{}
	h.Set("Origin", base.Scheme+"://"+base.Host)
	h.Set("Cookie", cookie+"="+tk.Token)
	conn, resp, err := websocket.DefaultDialer.Dial(scheme+"://"+base.Host+"/api/ws", h)
	if err != nil {
		if resp != nil {
			return fmt.Errorf("dial: %w (%s)", err, resp.Status)
		}
		return fmt.Errorf("dial: %w", err)
	}
	m.conn = conn
	pc, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		return err
	}
	m.pc = pc
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
	pc.OnTrack(func(tr *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		video := tr.Kind() == webrtc.RTPCodecTypeVideo
		if !video {
			m.audioT.Add(1)
		}
		buf := make([]byte, 1600)
		for {
			n, _, err := tr.Read(buf)
			if err != nil {
				return
			}
			if video {
				m.videoMs.CompareAndSwap(0, time.Since(m.startedAt).Milliseconds()+1)
				m.videoB.Add(int64(n))
			} else {
				m.audioB.Add(int64(n))
			}
		}
	})
	if os.Getenv("LEGACY_SIGNALLING") == "1" {
		m.candMu.Lock()
		m.remoteSet, m.answerSent = true, true
		m.candMu.Unlock()
	}
	m.startedAt = time.Now()
	m.send("voice_join", map[string]any{"channel_id": channel})
	if d := iceTimeout(); d > 0 {
		go m.watchConnect(d)
	}
	m.send("voice_mute_state", map[string]any{"muted": true, "deafened": false})
	go func() {
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		for {
			sub := map[string]any{"kind": "screen", "user_id": sharer, "on": true}
			if q := m.quality(); q != nil {
				sub["quality"] = q
			}
			m.send("webrtc_subscribe", sub)
			select {
			case <-t.C:
			case <-m.stop:
				return
			}
		}
	}()
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
			m.offers.Add(1)
			m.event("offer %d received", m.offers.Load())
			var offer webrtc.SessionDescription
			if json.Unmarshal(ev.Payload, &offer) != nil {
				continue
			}
			if err := pc.SetRemoteDescription(offer); err != nil {
				continue
			}
			m.remoteApplied()
			answer, err := pc.CreateAnswer(nil)
			if err != nil {
				continue
			}
			if err := pc.SetLocalDescription(answer); err != nil {
				continue
			}
			m.send("webrtc_answer", pc.LocalDescription())
			m.event("answer sent")
			m.answerWasSent()
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

func envInt(k string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(k)); err == nil {
		return v
	}
	return def
}

func main() {
	if err := mainErr(); err != nil {
		fmt.Fprintln(os.Stderr, "viewer:", err)
		os.Exit(2)
	}
}

func mainErr() error {
	base, err := url.Parse(os.Getenv("BASE_URL"))
	if err != nil || base.Host == "" {
		return errors.New("BASE_URL is required")
	}
	var tokens []token
	raw, err := os.ReadFile(os.Getenv("TOKENS"))
	if err != nil {
		return fmt.Errorf("TOKENS: %w", err)
	}
	if err := json.Unmarshal(raw, &tokens); err != nil {
		return fmt.Errorf("TOKENS: %w", err)
	}
	from := envInt("FROM", 0)
	to := min(envInt("TO", len(tokens)), len(tokens))
	hold := time.Duration(envInt("HOLD_S", 60)) * time.Second
	spacing := time.Duration(envInt("SPACING_MS", 300)) * time.Millisecond
	loss, _ := strconv.ParseFloat(os.Getenv("LOSS"), 64)
	jitter := time.Duration(envInt("JITTER_MS", 0)) * time.Millisecond
	channel, sharer := os.Getenv("CHANNEL_ID"), os.Getenv("SHARER_ID")
	if channel == "" || sharer == "" {
		return errors.New("CHANNEL_ID and SHARER_ID are required")
	}
	api, err := newAPI(loss, jitter)
	if err != nil {
		return err
	}

	var members []*member
	for i := from; i < to; i++ {
		m := &member{id: tokens[i].ID, index: i, stop: make(chan struct{})}
		members = append(members, m)
		go func() {
			if err := run(api, base, tokens[i], channel, sharer, m); err != nil {
				select {
				case <-m.stop: // closing at the end
				default:
					m.failed.Store(err.Error())
				}
			}
		}()
		time.Sleep(spacing)
	}

	// Sample every 250 ms: stall = longest stretch without new video bytes
	// after the first ones.
	start := time.Now()
	last := map[*member]int64{}
	lastGrow := map[*member]time.Time{}
	next := start.Add(10 * time.Second)
	stopFile := os.Getenv("STOP_FILE")
	for time.Since(start) < hold {
		time.Sleep(250 * time.Millisecond)
		now := time.Now()
		if stopFile != "" {
			if _, err := os.Stat(stopFile); err == nil {
				break
			}
		}
		for _, m := range members {
			v := m.videoB.Load()
			g, started := lastGrow[m]
			if v > last[m] {
				last[m] = v
				lastGrow[m] = now
			}
			if started {
				if d := now.Sub(g).Milliseconds(); d > m.maxStallMs.Load() {
					m.maxStallMs.Store(d)
				}
			}
		}
		if now.Before(next) {
			continue
		}
		next = now.Add(10 * time.Second)
		var connected, failed, receiving int
		var offers int64
		for _, m := range members {
			if m.connected.Load() {
				connected++
			}
			if g, ok := lastGrow[m]; ok && now.Sub(g) < 3*time.Second {
				receiving++
			}
			offers += m.offers.Load()
			if m.failed.Load() != nil {
				failed++
			}
		}
		fmt.Printf("%s viewers=%d connected=%d receivingScreen=%d failed=%d offers=%d\n",
			now.Format("15:04:05"), len(members), connected, receiving, failed, offers)
	}

	// Take every member's ICE snapshot (bounded: Pion's statistics can block on
	// a stuck connection) before anything is closed, then write the result,
	// and only then tear down.
	diagnoseAll(members, 5*time.Second)
	res := summarize(members, loss, jitter)
	if out := os.Getenv("OUT"); out != "" {
		b, _ := json.MarshalIndent(res, "", " ")
		if err := os.WriteFile(out, b, 0o600); err != nil {
			return err
		}
	}
	for _, mr := range res.PerMember {
		if mr.EverConnected {
			continue
		}
		fmt.Printf("member %s never connected (err=%s)\n", mr.ID[:min(8, len(mr.ID))], mr.Error)
		for _, m := range members {
			if m.id == mr.ID {
				for _, l := range m.diagLines() {
					fmt.Println(l)
				}
			}
		}
	}
	fmt.Printf("viewers=%d neverConnected=%d iceTimedOut=%d neverReceived=%d failed=%d flaps=%d connect p50/p95=%d/%dms firstVideo p50/p95=%d/%dms maxStall=%dms avgKbps=%.0f\n",
		res.Members, res.NeverConnected, res.ICETimedOut, res.NeverReceived, res.Failed, res.Flaps,
		res.ConnectMsP50, res.ConnectMsP95, res.FirstVideoMsP50, res.FirstVideoMsP95, res.MaxStallMs, res.AvgVideoKbps)

	// Closing is bounded: a Pion Close can wait forever on a connection whose
	// ICE start never finishes. The process exits right after, which drops
	// whatever is left.
	limit := time.Duration(envInt("CLOSE_TIMEOUT_S", 10)) * time.Second
	if n := closeAll(members, limit); n < len(members) {
		fmt.Printf("close: %d of %d members did not close within %s\n", len(members)-n, len(members), limit)
	}
	return nil
}
