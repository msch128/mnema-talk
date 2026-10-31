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
//
// Headless publishers (synthetic VP8 and Opus, no devices): the first
// SCREEN_PUBLISHERS tokens of the slice share a screen, the next
// CAMERA_PUBLISHERS publish a camera and the next AUDIO_PUBLISHERS speak.
// Every member watches the screens (SHARER_ID, a comma separated list, adds
// ones published elsewhere; the screen publishers' ids are added). Cameras
// reach everybody by default; WATCH_CAMERAS=<n> keeps n of them visible per
// member (the others are opted out), "none" hides all.
//
//	SCREEN_KBPS, SCREEN_FPS   shape of a shared screen (default 1500, 15)
//	CAMERA_KBPS, CAMERA_FPS   shape of a camera (default 300, 24)
//	KEYFRAME_S    seconds between keyframes (default 3)
//	FREEZE_MS     a video gap above this is a freeze (default 1000)
//	RELAY=1       relay mode: ICE servers from /api/webrtc/config, relayed
//	              candidates only, so all media goes through the built-in TURN
//	              relay; RELAY_HOST replaces the host of the TURN URLs
//	SESSION_COOKIE  cookie name when PUBLIC_URL is https but BASE_URL is not
//	METRICS_TOKEN server METRICS_TOKEN: samples CPU and memory every 5 s
//	EVALUATE=1    judge the run against thresholds and exit 1 on a violation
//	              (MAX_NEVER_CONNECTED, MAX_NEVER_RECEIVED, MAX_LOSS_PCT,
//	              MAX_FROZEN_PCT, MIN_BITRATE_PCT, MAX_FLAPS, MAX_CPU_CORES,
//	              MAX_RSS_MB, MAX_GOROUTINE_LEAK; see loadtest/README.md)
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/msch128/mnema-talk/loadtest/viewer/internal/lt"
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

func envInt(k string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(k)); err == nil {
		return v
	}
	return def
}

func main() {
	ok, err := mainErr()
	if err != nil {
		fmt.Fprintln(os.Stderr, "viewer:", err)
		os.Exit(2)
	}
	if !ok {
		os.Exit(1)
	}
}

// roleCounts is how many members of the slice publish what.
type roleCounts struct {
	screens, cameras, audio int
}

// roleOf assigns the roles by position in the slice: screens first, then
// cameras, then speakers, the rest only watch.
func (rc roleCounts) roleOf(pos int) pubRole {
	switch {
	case pos < rc.screens:
		return pubRole{screen: true}
	case pos < rc.screens+rc.cameras:
		return pubRole{camera: true}
	case pos < rc.screens+rc.cameras+rc.audio:
		return pubRole{audio: true}
	}
	return pubRole{}
}

// viewerConfig is what the environment asks of this run.
type viewerConfig struct {
	base          *url.URL
	tokens        []token
	from, to      int
	hold, spacing time.Duration
	loss          float64
	jitter        time.Duration
	channel       string
	sharers       []string // users whose screen every member watches
	roles         roleCounts
	cameras       cameraView // WATCH_CAMERAS
	relay         bool
	relayHost     string
	metrics       string
}

func loadConfig() (viewerConfig, error) {
	var c viewerConfig
	base, err := lt.ParseBase(os.Getenv("BASE_URL"))
	if err != nil {
		return c, err
	}
	c.base = base
	if c.tokens, err = lt.LoadTokens(os.Getenv("TOKENS")); err != nil {
		return c, err
	}
	c.from = envInt("FROM", 0)
	c.to = min(envInt("TO", len(c.tokens)), len(c.tokens))
	c.hold = time.Duration(envInt("HOLD_S", 60)) * time.Second
	c.relay, c.relayHost = lt.Bool("RELAY"), os.Getenv("RELAY_HOST")
	c.spacing = time.Duration(envInt("SPACING_MS", defaultSpacingMs(c.relay))) * time.Millisecond
	if loss, err := strconv.ParseFloat(os.Getenv("LOSS"), 64); err == nil {
		c.loss = loss
	}
	c.jitter = time.Duration(envInt("JITTER_MS", 0)) * time.Millisecond
	c.channel = os.Getenv("CHANNEL_ID")
	c.roles = roleCounts{envInt("SCREEN_PUBLISHERS", 0), envInt("CAMERA_PUBLISHERS", 0), envInt("AUDIO_PUBLISHERS", 0)}
	if c.cameras, err = parseCameraView(os.Getenv("WATCH_CAMERAS")); err != nil {
		return c, err
	}
	c.metrics = os.Getenv("METRICS_TOKEN")
	c.sharers = sharerIDs(c)
	return c, c.validate()
}

// defaultSpacingMs is the pause between two joins. The built-in relay admits
// 120 allocations a minute per address (and 60 at once), so relay mode joins
// slower.
func defaultSpacingMs(relay bool) int {
	if relay {
		return 600
	}
	return 300
}

// sharerIDs lists the screens every member watches: SHARER_ID (comma
// separated) and the synthetic screen publishers.
func sharerIDs(c viewerConfig) []string {
	var ids []string
	for _, id := range strings.Split(os.Getenv("SHARER_ID"), ",") {
		if id = strings.TrimSpace(id); id != "" {
			ids = append(ids, id)
		}
	}
	for i := range c.roles.screens {
		if c.from+i < len(c.tokens) {
			ids = append(ids, c.tokens[c.from+i].ID)
		}
	}
	return ids
}

func (c viewerConfig) validate() error {
	if c.channel == "" {
		return errors.New("CHANNEL_ID is required")
	}
	if len(c.sharers) == 0 && c.roles.cameras == 0 {
		return errors.New("nothing to watch: set SHARER_ID, SCREEN_PUBLISHERS or CAMERA_PUBLISHERS")
	}
	n := c.roles.screens + c.roles.cameras + c.roles.audio
	if n > c.to-c.from {
		return fmt.Errorf("%d publishers asked for, but the slice has %d members", n, c.to-c.from)
	}
	return nil
}

// cameraIDs lists the users that publish a camera.
func (c viewerConfig) cameraIDs() []string {
	var ids []string
	for i := range c.roles.cameras {
		ids = append(ids, c.tokens[c.from+c.roles.screens+i].ID)
	}
	return ids
}

// memberIDs lists the users of the slice in order.
func (c viewerConfig) memberIDs() []string {
	var ids []string
	for _, t := range c.tokens[c.from:c.to] {
		ids = append(ids, t.ID)
	}
	return ids
}

// startMembers starts one member per token in the configured slice, spaced
// apart.
func startMembers(api *webrtc.API, c viewerConfig) []*member {
	var members []*member
	for i := c.from; i < c.to; i++ {
		m := &member{id: c.tokens[i].ID, index: i, stop: make(chan struct{}), api: api, role: c.roles.roleOf(i - c.from)}
		m.cameras = c.cameras.plan(m.id, i-c.from, c.cameraIDs())
		members = append(members, m)
		go func() {
			if err := run(api, c, c.tokens[i], m); err != nil {
				select {
				case <-m.stop: // closing at the end
				default:
					m.failed.Store(err.Error())
				}
			}
		}()
		time.Sleep(c.spacing)
	}
	return members
}

// writeResult writes the JSON result to OUT, if set.
func writeResult(res result) error {
	out := os.Getenv("OUT")
	if out == "" {
		return nil
	}
	b, err := json.MarshalIndent(res, "", " ")
	if err != nil {
		return err
	}
	return os.WriteFile(out, b, 0o600)
}

// mainErr runs the test and reports whether the thresholds (EVALUATE=1) were
// met.
func mainErr() (bool, error) {
	c, err := loadConfig()
	if err != nil {
		return false, err
	}
	api, err := newAPI(c.loss, c.jitter)
	if err != nil {
		return false, err
	}
	cpu := lt.StartCPUMeter()
	sampler := lt.StartSampler(lt.NewScraper(c.base, c.metrics), 5*time.Second)
	sampler.Mark("pre")
	sampler.SetPhase("ramp")
	members := startMembers(api, c)
	sampler.Mark("hold")
	watchMembers(members, c.hold, os.Getenv("STOP_FILE"))

	// Take every member's ICE snapshot (bounded: Pion's statistics can block on
	// a stuck connection) and the received media before anything is closed,
	// then write the result, and only then tear down.
	diagnoseAll(members, 5*time.Second)
	res := summarize(members, c.loss, c.jitter)
	sampler.SetPhase("settle")
	if err := writeResult(res); err != nil {
		return false, err
	}

	// Closing is bounded: a Pion Close can wait forever on a connection whose
	// ICE start never finishes. The process exits right after, which drops
	// whatever is left.
	limit := time.Duration(envInt("CLOSE_TIMEOUT_S", 10)) * time.Second
	closed := closeAll(members, limit)
	time.Sleep(5 * time.Second) // the server finishes tearing the members down
	sampler.Mark("post")
	samples, scrapeErrors := sampler.Stop()
	finishResult(&res, c, lt.SummarizeServer(samples, scrapeErrors))
	res.GeneratorCores = cpu.Cores()

	if err := writeResult(res); err != nil {
		return false, err
	}
	printReport(members, res)
	if closed < len(members) {
		fmt.Printf("close: %d of %d members did not close within %s\n", len(members)-closed, len(members), limit)
	}
	return res.OK, nil
}
