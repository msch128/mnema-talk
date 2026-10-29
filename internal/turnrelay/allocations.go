package turnrelay

import (
	"context"
	"net"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/metrics"
)

// DropMember closes every allocation of a member, for when they may no longer
// join voice.
func (s *Server) DropMember(user uuid.UUID) {
	s.mu.Lock()
	var drop []*guard
	for g := range s.guards {
		if g.user == user {
			drop = append(drop, g)
		}
	}
	s.mu.Unlock()
	for _, g := range drop {
		_ = g.Close()
	}
}

// maintain refreshes the SFU's addresses and closes allocations that idled or
// outlived their credentials.
func (s *Server) maintain(ctx context.Context) {
	defer s.wg.Done()
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		s.refreshTarget()
		s.reap()
	}
}

func (s *Server) reap() {
	now := s.now()
	s.mu.Lock()
	var stale []*guard
	for g := range s.guards {
		idle := now.Sub(time.Unix(0, g.lastActive.Load()))
		if !now.Before(g.expires) || idle > s.limits.IdleTimeout {
			stale = append(stale, g)
		}
	}
	s.mu.Unlock()
	for _, g := range stale {
		s.debug("turn: closing allocation", "user", g.user)
		_ = g.Close()
	}
}

// acquire takes a place for a new allocation of a member.
func (s *Server) acquire(user uuid.UUID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case len(s.guards) >= s.limits.Total:
		metrics.TURNEvents.With("quota_total").Inc()
		return false
	case s.perUser[user] >= s.limits.PerMember:
		metrics.TURNEvents.With("quota_user").Inc()
		return false
	}
	s.perUser[user]++
	return true
}

func (s *Server) register(g *guard) {
	s.mu.Lock()
	s.guards[g] = struct{}{}
	s.byPort[g.port] = g
	s.mu.Unlock()
	metrics.TURNAllocations.Add(1)
}

// unacquire gives back a place taken by acquire that never became an allocation.
func (s *Server) unacquire(user uuid.UUID) {
	s.mu.Lock()
	if s.perUser[user]--; s.perUser[user] <= 0 {
		delete(s.perUser, user)
	}
	s.mu.Unlock()
}

// release forgets a closed allocation and returns its place.
func (s *Server) release(g *guard) {
	s.mu.Lock()
	if _, ok := s.guards[g]; ok {
		delete(s.guards, g)
		delete(s.byPort, g.port)
		for k, v := range s.byClient {
			if v == g {
				delete(s.byClient, k)
			}
		}
		if s.perUser[g.user]--; s.perUser[g.user] <= 0 {
			delete(s.perUser, g.user)
		}
		metrics.TURNAllocations.Add(-1)
	}
	s.mu.Unlock()
}

func hostOf(a net.Addr) string {
	if a == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(a.String())
	if err != nil {
		return a.String()
	}
	return host
}
