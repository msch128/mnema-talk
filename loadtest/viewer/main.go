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
//	LEGACY_SIGNALLING=1  diagnostic: send candidates before the answer and
//	              apply remote ones without a remote description (the
//	              pre-0.8 scratch client), to reproduce ICE failures
//	QUALITY       layer of the screen share to watch: "auto" (default), a
//	              layer index 0..2 (0 is the best), or "spread" to give each
//	              viewer layer index%3, so a run exercises all layers
//	STOP_FILE     optional path; the run ends early once the file exists
//	OUT           optional path of the JSON result
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/interceptor"
	"github.com/pion/webrtc/v4"
)

type token struct {
	ID    string `json:"id"`
	Token string `json:"token"`
}

// quality is the layer this member asks for (QUALITY), nil to leave it to the
// server.
func (m *member) quality() any {
	switch q := os.Getenv("QUALITY"); q {
	case "", "auto":
		return nil
	case "spread":
		return m.index % 3
	default:
		if n, err := strconv.Atoi(q); err == nil && n >= 0 && n < 3 {
			return n
		}
		return nil
	}
}

// member is one simulated Talk participant.
type member struct {
	id    string
	index int

	conn    *websocket.Conn
	pc      *webrtc.PeerConnection
	writeMu sync.Mutex
	stop    chan struct{}

	startedAt time.Time // voice_join sent
	connected atomic.Bool
	everConn  atomic.Bool
	connMs    atomic.Int64 // join -> first "connected"
	videoMs   atomic.Int64 // join -> first video byte, plus one
	flaps     atomic.Int64 // connected -> not connected transitions

	videoB atomic.Int64
	audioB atomic.Int64
	audioT atomic.Int64
	offers atomic.Int64

	candSent, candRecv, candErr atomic.Int64
	maxStallMs                  atomic.Int64

	failed atomic.Value // string

	// Remote candidates that arrive before the offer is applied, and local
	// candidates gathered before the answer is on its way, are queued:
	// pion rejects AddICECandidate without a remote description, and the
	// server rejects candidates before it has the answer.
	candMu     sync.Mutex
	remoteSet  bool
	pendingIn  []webrtc.ICECandidateInit
	answerSent bool
	pendingOut []webrtc.ICECandidateInit
	closeOnce  sync.Once
}

func (m *member) send(kind string, payload any) {
	m.writeMu.Lock()
	defer m.writeMu.Unlock()
	if m.conn == nil {
		return
	}
	_ = m.conn.WriteJSON(map[string]any{"type": kind, "payload": payload})
}

func (m *member) close() {
	m.closeOnce.Do(func() {
		close(m.stop)
		m.send("voice_leave", map[string]any{})
		if m.conn != nil {
			_ = m.conn.Close()
		}
		if m.pc != nil {
			_ = m.pc.Close()
		}
	})
}

// lossFactory drops incoming RTP before the NACK generator (the factory is
// first in the chain, i.e. closest to the network), so the receiver reports
// the gaps and the SFU's retransmission path is exercised.
type lossFactory struct {
	loss   float64
	jitter time.Duration
}

func (f *lossFactory) NewInterceptor(string) (interceptor.Interceptor, error) {
	return &lossInterceptor{loss: f.loss, jitter: f.jitter}, nil
}

type lossInterceptor struct {
	interceptor.NoOp
	loss   float64
	jitter time.Duration
}

func (l *lossInterceptor) BindRemoteStream(_ *interceptor.StreamInfo, reader interceptor.RTPReader) interceptor.RTPReader {
	return interceptor.RTPReaderFunc(func(b []byte, a interceptor.Attributes) (int, interceptor.Attributes, error) {
		for {
			n, attr, err := reader.Read(b, a)
			if err != nil {
				return n, attr, err
			}
			if l.jitter > 0 {
				time.Sleep(time.Duration(rand.Int64N(int64(l.jitter))))
			}
			if l.loss > 0 && rand.Float64() < l.loss {
				continue
			}
			return n, attr, nil
		}
	})
}

func newAPI(loss float64, jitter time.Duration) (*webrtc.API, error) {
	me := &webrtc.MediaEngine{}
	if err := me.RegisterDefaultCodecs(); err != nil {
		return nil, err
	}
	reg := &interceptor.Registry{}
	if loss > 0 || jitter > 0 {
		reg.Add(&lossFactory{loss: loss, jitter: jitter})
	}
	if err := webrtc.RegisterDefaultInterceptors(me, reg); err != nil {
		return nil, err
	}
	return webrtc.NewAPI(webrtc.WithMediaEngine(me), webrtc.WithInterceptorRegistry(reg)), nil
}

// addRemote applies a candidate, or queues it until the offer is applied.
func (m *member) addRemote(c webrtc.ICECandidateInit) {
	m.candMu.Lock()
	defer m.candMu.Unlock()
	if !m.remoteSet {
		m.pendingIn = append(m.pendingIn, c)
		return
	}
	m.candRecv.Add(1)
	if err := m.pc.AddICECandidate(c); err != nil {
		m.candErr.Add(1)
	}
}

func (m *member) remoteApplied() {
	m.candMu.Lock()
	defer m.candMu.Unlock()
	m.remoteSet = true
	for _, c := range m.pendingIn {
		m.candRecv.Add(1)
		if err := m.pc.AddICECandidate(c); err != nil {
			m.candErr.Add(1)
		}
	}
	m.pendingIn = nil
}

// sendCandidate sends a local candidate once the answer has been sent.
func (m *member) sendCandidate(c webrtc.ICECandidateInit) {
	m.candMu.Lock()
	if !m.answerSent {
		m.pendingOut = append(m.pendingOut, c)
		m.candMu.Unlock()
		return
	}
	m.candMu.Unlock()
	m.candSent.Add(1)
	m.send("webrtc_candidate", c)
}

func (m *member) answerWasSent() {
	m.candMu.Lock()
	m.answerSent = true
	out := m.pendingOut
	m.pendingOut = nil
	m.candMu.Unlock()
	for _, c := range out {
		m.candSent.Add(1)
		m.send("webrtc_candidate", c)
	}
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
			m.sendCandidate(c.ToJSON())
		}
	})
	pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
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

type memberResult struct {
	ID            string `json:"id"`
	Connected     bool   `json:"connected"`
	EverConnected bool   `json:"ever_connected"`
	ConnectMs     int64  `json:"connect_ms"`
	FirstVideoMs  int64  `json:"first_video_ms"`
	VideoBytes    int64  `json:"video_bytes"`
	AudioTracks   int64  `json:"audio_tracks"`
	Offers        int64  `json:"offers"`
	Flaps         int64  `json:"flaps"`
	CandSent      int64  `json:"candidates_sent"`
	CandRecv      int64  `json:"candidates_received"`
	CandErrors    int64  `json:"candidate_errors"`
	MaxStallMs    int64  `json:"max_stall_ms"`
	Error         string `json:"error,omitempty"`
}

type result struct {
	Members         int            `json:"members"`
	NeverConnected  int            `json:"never_connected"`
	NeverReceived   int            `json:"never_received"`
	Failed          int            `json:"failed"`
	Flaps           int64          `json:"flaps"`
	ConnectMsP50    int64          `json:"connect_ms_p50"`
	ConnectMsP95    int64          `json:"connect_ms_p95"`
	FirstVideoMsP50 int64          `json:"first_video_ms_p50"`
	FirstVideoMsP95 int64          `json:"first_video_ms_p95"`
	MaxStallMs      int64          `json:"max_stall_ms"`
	AvgVideoKbps    float64        `json:"avg_video_kbps"`
	CandidateErrors int64          `json:"candidate_errors"`
	Loss            float64        `json:"loss"`
	JitterMs        int64          `json:"jitter_ms"`
	PerMember       []memberResult `json:"per_member"`
}

func pct(v []int64, q float64) int64 {
	if len(v) == 0 {
		return 0
	}
	sort.Slice(v, func(i, j int) bool { return v[i] < v[j] })
	return v[min(len(v)-1, int(q*float64(len(v))))]
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

	res := result{Members: len(members), Loss: loss, JitterMs: jitter.Milliseconds()}
	var conns, firsts []int64
	var kbps float64
	var kbpsN int
	for _, m := range members {
		mr := memberResult{
			ID: m.id, Connected: m.connected.Load(), EverConnected: m.everConn.Load(),
			ConnectMs: m.connMs.Load(), VideoBytes: m.videoB.Load(), AudioTracks: m.audioT.Load(),
			Offers: m.offers.Load(), Flaps: m.flaps.Load(), CandSent: m.candSent.Load(),
			CandRecv: m.candRecv.Load(), CandErrors: m.candErr.Load(), MaxStallMs: m.maxStallMs.Load(),
		}
		if v := m.videoMs.Load(); v > 0 {
			mr.FirstVideoMs = v - 1
			firsts = append(firsts, mr.FirstVideoMs)
		}
		if f, ok := m.failed.Load().(string); ok {
			mr.Error = f
			res.Failed++
		}
		if !mr.EverConnected {
			res.NeverConnected++
		} else {
			conns = append(conns, mr.ConnectMs)
		}
		if mr.VideoBytes == 0 {
			res.NeverReceived++
		} else {
			kbps += float64(mr.VideoBytes) * 8 / time.Since(m.startedAt).Seconds() / 1000
			kbpsN++
		}
		res.Flaps += mr.Flaps
		res.CandidateErrors += mr.CandErrors
		res.MaxStallMs = max(res.MaxStallMs, mr.MaxStallMs)
		res.PerMember = append(res.PerMember, mr)
	}
	res.ConnectMsP50, res.ConnectMsP95 = pct(conns, 0.5), pct(conns, 0.95)
	res.FirstVideoMsP50, res.FirstVideoMsP95 = pct(firsts, 0.5), pct(firsts, 0.95)
	if kbpsN > 0 {
		res.AvgVideoKbps = kbps / float64(kbpsN)
	}
	for _, m := range members {
		if !m.everConn.Load() {
			fmt.Printf("member %s never connected (err=%v)\n", m.id[:min(8, len(m.id))], m.failed.Load())
		}
		m.close()
	}
	if out := os.Getenv("OUT"); out != "" {
		b, _ := json.MarshalIndent(res, "", " ")
		if err := os.WriteFile(out, b, 0o600); err != nil {
			return err
		}
	}
	fmt.Printf("viewers=%d neverConnected=%d neverReceived=%d failed=%d flaps=%d connect p50/p95=%d/%dms firstVideo p50/p95=%d/%dms maxStall=%dms avgKbps=%.0f\n",
		res.Members, res.NeverConnected, res.NeverReceived, res.Failed, res.Flaps,
		res.ConnectMsP50, res.ConnectMsP95, res.FirstVideoMsP50, res.FirstVideoMsP95, res.MaxStallMs, res.AvgVideoKbps)
	time.Sleep(500 * time.Millisecond)
	return nil
}
