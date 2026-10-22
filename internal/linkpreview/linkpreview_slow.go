package linkpreview

import (
	"context"
	"errors"
	"net"
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
