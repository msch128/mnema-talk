package main

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"net/url"
	"os"
	"time"

	"github.com/mnema-xyz/talk/loadtest/viewer/internal/lt"
)

type config struct {
	base      *url.URL
	tokens    []lt.Token
	channel   string
	cookie    string
	sessions  int
	backoff   string
	perSecond int
	readyFile string
	timeout   time.Duration
	out       string
}

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
	c.sessions = lt.Int("SESSIONS", 1000)
	if c.sessions < 1 || c.sessions > len(c.tokens) {
		return c, fmt.Errorf("SESSIONS=%d, TOKENS has %d entries", c.sessions, len(c.tokens))
	}
	switch c.backoff = os.Getenv("BACKOFF"); c.backoff {
	case "":
		c.backoff = "half"
	case "half", "fixed", "full":
	default:
		return c, fmt.Errorf("BACKOFF=%q: half, fixed or full", c.backoff)
	}
	c.perSecond = max(lt.Int("CONNECT_PER_S", 100), 1)
	c.readyFile = os.Getenv("READY_FILE")
	c.timeout = time.Duration(lt.Int("TIMEOUT_S", 300)) * time.Second
	c.out = os.Getenv("OUT")
	return c, nil
}

// The web client's backoff (stores/socket.ts): the first retry after a second,
// doubling up to 30 s, reset once a socket opens.
const (
	backoffBase = time.Second
	backoffCap  = 30 * time.Second
)

// wait is how long a session waits before the retry whose window is d:
// half waits a random point in the second half of the window (reconnectWait in
// stores/socket.ts), fixed the whole window (the client before), full a
// random point anywhere in it.
func (c config) wait(d time.Duration) time.Duration {
	switch c.backoff {
	case "fixed":
		return d
	case "full":
		return time.Duration(rand.Int64N(int64(d) + 1))
	}
	return d/2 + time.Duration(rand.Int64N(int64(d/2)+1))
}

func nextWindow(d time.Duration) time.Duration { return min(d*2, backoffCap) }
