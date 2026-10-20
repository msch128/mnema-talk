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
	"net/url"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

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
	if err := mainErr(); err != nil {
		fmt.Fprintln(os.Stderr, "viewer:", err)
		os.Exit(2)
	}
}

// viewerConfig is what the environment asks of this run.
type viewerConfig struct {
	base            *url.URL
	tokens          []token
	from, to        int
	hold, spacing   time.Duration
	loss            float64
	jitter          time.Duration
	channel, sharer string
}

func loadConfig() (viewerConfig, error) {
	var c viewerConfig
	base, err := url.Parse(os.Getenv("BASE_URL"))
	if err != nil || base.Host == "" {
		return c, errors.New("BASE_URL is required")
	}
	c.base = base
	raw, err := os.ReadFile(os.Getenv("TOKENS"))
	if err != nil {
		return c, fmt.Errorf("TOKENS: %w", err)
	}
	if err := json.Unmarshal(raw, &c.tokens); err != nil {
		return c, fmt.Errorf("TOKENS: %w", err)
	}
	c.from = envInt("FROM", 0)
	c.to = min(envInt("TO", len(c.tokens)), len(c.tokens))
	c.hold = time.Duration(envInt("HOLD_S", 60)) * time.Second
	c.spacing = time.Duration(envInt("SPACING_MS", 300)) * time.Millisecond
	if loss, err := strconv.ParseFloat(os.Getenv("LOSS"), 64); err == nil {
		c.loss = loss
	}
	c.jitter = time.Duration(envInt("JITTER_MS", 0)) * time.Millisecond
	c.channel, c.sharer = os.Getenv("CHANNEL_ID"), os.Getenv("SHARER_ID")
	if c.channel == "" || c.sharer == "" {
		return c, errors.New("CHANNEL_ID and SHARER_ID are required")
	}
	return c, nil
}

// startMembers starts one member per token in the configured slice, spaced
// apart.
func startMembers(api *webrtc.API, c viewerConfig) []*member {
	var members []*member
	for i := c.from; i < c.to; i++ {
		m := &member{id: c.tokens[i].ID, index: i, stop: make(chan struct{})}
		members = append(members, m)
		go func() {
			if err := run(api, c.base, c.tokens[i], c.channel, c.sharer, m); err != nil {
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

func mainErr() error {
	c, err := loadConfig()
	if err != nil {
		return err
	}
	api, err := newAPI(c.loss, c.jitter)
	if err != nil {
		return err
	}
	members := startMembers(api, c)
	watchMembers(members, c.hold, os.Getenv("STOP_FILE"))

	// Take every member's ICE snapshot (bounded: Pion's statistics can block on
	// a stuck connection) before anything is closed, then write the result,
	// and only then tear down.
	diagnoseAll(members, 5*time.Second)
	res := summarize(members, c.loss, c.jitter)
	if out := os.Getenv("OUT"); out != "" {
		b, err := json.MarshalIndent(res, "", " ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(out, b, 0o600); err != nil {
			return err
		}
	}
	printReport(members, res)

	// Closing is bounded: a Pion Close can wait forever on a connection whose
	// ICE start never finishes. The process exits right after, which drops
	// whatever is left.
	limit := time.Duration(envInt("CLOSE_TIMEOUT_S", 10)) * time.Second
	if n := closeAll(members, limit); n < len(members) {
		fmt.Printf("close: %d of %d members did not close within %s\n", len(members)-n, len(members), limit)
	}
	return nil
}
