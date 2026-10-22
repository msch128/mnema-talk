package linkpreview

import (
	"context"
	"errors"
	"net"
	"slices"
	"sync"
	"time"
)

// errSlowHost answers a link whose host recently ran into the fetch deadline.
var errSlowHost = errors.New("host answers too slowly")

const (
	// slowHostTTL is how long a host that hit the deadline is not fetched again.
	slowHostTTL = 5 * time.Minute
	// slowHostMax bounds the table; when full, expired entries go first.
	slowHostMax = 1024
)

// slowHosts remembers hosts that held a fetch until its deadline, so a
// slow-drip host costs one slot for one deadline, not for every request.
type slowHosts struct {
	mu sync.Mutex
	m  map[string]time.Time
}

func (s *slowHosts) is(host string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	until, ok := s.m[host]
	if ok && time.Now().After(until) {
		delete(s.m, host)
		return false
	}
	return ok
}

// noteTimeout marks host when err is the fetch deadline running out.
func (s *slowHosts) noteTimeout(host string, err error) {
	var ne net.Error
	if !errors.Is(err, context.DeadlineExceeded) && !(errors.As(err, &ne) && ne.Timeout()) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m == nil {
		s.m = map[string]time.Time{}
	}
	if len(s.m) >= slowHostMax {
		now := time.Now()
		for h, until := range s.m {
			if now.After(until) {
				delete(s.m, h)
			}
		}
		if len(s.m) >= slowHostMax {
			return
		}
	}
	s.m[host] = time.Now().Add(slowHostTTL)
}

// hopTimer times each hop of one fetch (the posted link and every redirect
// target), so that a timeout is held against the hosts that actually used up
// the time. Without it, a host that drips for nearly the whole deadline and
// then redirects would get the innocent redirect target marked.
type hopTimer struct {
	mu    sync.Mutex
	host  string
	start time.Time
	done  []hopSpan
}

type hopSpan struct {
	host string
	took time.Duration
}

type hopTimerKey struct{}

func newHopTimer(host string) *hopTimer {
	return &hopTimer{host: host, start: time.Now()}
}

// next closes the current hop and starts one at host.
func (h *hopTimer) next(host string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := time.Now()
	h.done = append(h.done, hopSpan{h.host, now.Sub(h.start)})
	h.host, h.start = host, now
}

// noteTimeout marks every host whose own hop took at least half the fetch
// deadline, when err is that deadline running out.
func (h *hopTimer) noteTimeout(table *slowHosts, err error) {
	h.mu.Lock()
	spans := append(slices.Clone(h.done), hopSpan{h.host, time.Since(h.start)})
	h.mu.Unlock()
	for _, sp := range spans {
		if sp.took >= fetchTimeout/2 {
			table.noteTimeout(sp.host, err)
		}
	}
}

// lastHop returns the timer carried by ctx (the redirect check gets the
// request's context).
func lastHop(ctx context.Context) *hopTimer {
	h, _ := ctx.Value(hopTimerKey{}).(*hopTimer)
	return h
}
