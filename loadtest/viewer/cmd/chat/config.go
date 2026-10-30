package main

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"time"

	"github.com/msch128/mnema-talk/loadtest/viewer/internal/lt"
)

// config is what the environment asks of one run.
type config struct {
	base      *url.URL
	tokens    []lt.Token
	channel   string
	cookie    string
	sessions  int
	writers   int
	rate      float64 // messages per second per writer
	typing    bool
	reactPct  int
	editPct   int
	msgBytes  int
	history   bool
	rampPerMn int
	spoofXFF  bool
	hold      time.Duration
	settle    time.Duration
	out       string
	metrics   string
	th        thresholds
}

// The server allows one account 600 API requests a minute, and one address
// 120 WebSocket upgrades a minute.
const (
	apiPerMinute        = 600
	upgradesPerMinute   = 120
	defaultRampPerMin   = 100
	defaultMessageBytes = 80
)

func loadConfig() (config, error) {
	var c config
	base, err := lt.ParseBase(os.Getenv("BASE_URL"))
	if err != nil {
		return c, err
	}
	c.base = base
	if c.tokens, err = lt.LoadTokens(os.Getenv("TOKENS")); err != nil {
		return c, err
	}
	if c.channel = os.Getenv("CHANNEL_ID"); c.channel == "" {
		return c, errors.New("CHANNEL_ID (a text channel) is required")
	}
	c.cookie = lt.CookieName(base)
	c.sessions = lt.Int("SESSIONS", 100)
	if c.sessions < 1 || c.sessions > len(c.tokens) {
		return c, fmt.Errorf("SESSIONS=%d, TOKENS has %d entries", c.sessions, len(c.tokens))
	}
	c.writers = writerCount(c.sessions, lt.Float("WRITER_PCT", 10))
	c.rate = lt.Float("RATE", 0.2)
	c.typing = os.Getenv("TYPING") != "0"
	c.reactPct, c.editPct = lt.Int("REACT_PCT", 5), lt.Int("EDIT_PCT", 3)
	c.msgBytes = lt.Int("MSG_BYTES", defaultMessageBytes)
	c.history = os.Getenv("FETCH_HISTORY") != "0"
	c.rampPerMn = lt.Int("RAMP_PER_MIN", defaultRampPerMin)
	c.spoofXFF = lt.Bool("SPOOF_XFF")
	c.hold = time.Duration(lt.Int("HOLD_S", 120)) * time.Second
	c.settle = time.Duration(lt.Int("SETTLE_S", 10)) * time.Second
	c.out, c.metrics = os.Getenv("OUT"), os.Getenv("METRICS_TOKEN")
	c.th = loadThresholds()
	return c, c.validate()
}

// writerCount is the number of sessions that write: pct percent of them, at
// least one while pct is above zero.
func writerCount(sessions int, pct float64) int {
	if pct <= 0 {
		return 0
	}
	n := int(float64(sessions)*pct/100 + 0.5)
	return min(max(n, 1), sessions)
}

// validate refuses settings that would run into the server's own rate limits,
// which would measure the limiter instead of the server.
func (c config) validate() error {
	if c.writers > 0 && c.rate <= 0 {
		return errors.New("RATE must be above zero")
	}
	perMinute := c.rate * 60 * (1 + float64(c.reactPct+c.editPct)/100)
	if perMinute > apiPerMinute*0.9 {
		return fmt.Errorf("a writer would send about %.0f requests a minute; the server allows %d per account (lower RATE)", perMinute, apiPerMinute)
	}
	if c.rampPerMn > upgradesPerMinute && !c.spoofXFF {
		return fmt.Errorf("RAMP_PER_MIN=%d is above the %d WebSocket upgrades a minute the server allows per address (see SPOOF_XFF in the README)", c.rampPerMn, upgradesPerMinute)
	}
	if c.msgBytes < 24 || c.msgBytes > 3000 {
		return errors.New("MSG_BYTES must be between 24 and 3000")
	}
	return nil
}
