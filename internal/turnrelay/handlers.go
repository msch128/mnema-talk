package turnrelay

import (
	"net"

	"github.com/msch128/mnema-talk/internal/metrics"
	"github.com/pion/turn/v5"
)

// authenticate is the TURN auth handler: it checks the credentials and, for
// requests that start or extend an allocation, that the member may still join
// voice.
func (s *Server) authenticate(ra *turn.RequestAttributes) (string, []byte, bool) {
	now := s.now()
	ip := hostOf(ra.SrcAddr)
	// Failures are counted per source address and username: a UDP source can
	// be spoofed, and a budget per address alone would let anyone lock a real
	// user (or a whole office behind one NAT) out with forged requests. A
	// member with a valid username never fails here, so only broken or stale
	// credentials use up a budget, and only their own.
	key := ip + "|" + ra.Username
	if !s.failLimit.available(key, now) {
		metrics.TURNEvents.With("rate_limited").Inc()
		return "", nil, false
	}
	user, _, err := s.auth.check(ra.Username, now)
	if err != nil {
		s.failLimit.allow(key, now)
		metrics.TURNEvents.With("auth_failed").Inc()
		s.debug("turn: refused credentials", "from", ip, "err", err)
		return "", nil, false
	}
	if m := int(ra.Method); m == methodAllocate || m == methodRefresh {
		ok, err := (*s.check.Load())(user)
		switch {
		case err != nil && m == methodRefresh:
			// Could not tell: keep the running call rather than cut it.
			s.debug("turn: permission check failed, keeping the allocation", "user", user, "err", err)
		case err != nil || !ok:
			metrics.TURNEvents.With("forbidden").Inc()
			s.debug("turn: member may not join voice", "from", ip, "user", user, "err", err)
			if err == nil && m == methodRefresh {
				s.DropMember(user)
			}
			return "", nil, false
		}
	}
	// The credential's username doubles as the allocation's identity: it
	// carries the member and the expiry the generator needs.
	return ra.Username, turn.GenerateAuthKey(ra.Username, ra.Realm, s.auth.password(ra.Username)), true
}

// allowAllocation rate-limits allocation attempts per source address.
func (s *Server) allowAllocation(_, _ string, src net.Addr) bool {
	if !s.allocLimit.allow(hostOf(src), s.now()) {
		metrics.TURNEvents.With("rate_limited").Inc()
		s.debug("turn: allocation rate limit", "from", hostOf(src))
		return false
	}
	return true
}

// permit is the permission handler, run for every CreatePermission and
// ChannelBind: only the SFU's own addresses may be peers.
func (s *Server) permit(client net.Addr, peer net.IP) bool {
	ip := peer.To4()
	ok := ip != nil && s.target().isSFUAddress(ip)
	if !ok && ip != nil {
		s.refreshTarget() // the announced addresses may have just changed
		ok = s.target().isSFUAddress(ip)
	}
	if !ok {
		metrics.TURNEvents.With("peer_denied").Inc()
		s.debug("turn: refused a peer that is not the SFU", "client", client.String(), "peer", peer.String())
		return false
	}
	s.mu.Lock()
	g := s.byClient[client.String()]
	s.mu.Unlock()
	if g != nil {
		g.permit(ip)
	}
	return true
}

// events keeps count of the allocations.
func (s *Server) events() turn.EventHandler {
	return turn.EventHandler{
		OnAllocationCreated: func(src, _ net.Addr, _, _, _ string, relay net.Addr, _ int) {
			// Only a request that passed the integrity check ends in an
			// allocation, so this, and not the auth callback (which sees any
			// well-formed username), proves the TCP client holds a credential.
			if s.tcp != nil {
				s.tcp.authenticated(src)
			}
			udp, ok := relay.(*net.UDPAddr)
			if !ok {
				return
			}
			s.mu.Lock()
			if g := s.byPort[udp.Port]; g != nil {
				s.byClient[src.String()] = g
			}
			s.mu.Unlock()
			metrics.TURNEvents.With("allocation").Inc()
		},
		OnAllocationDeleted: func(src, _ net.Addr, _, _, _ string) {
			s.mu.Lock()
			g := s.byClient[src.String()]
			delete(s.byClient, src.String())
			s.mu.Unlock()
			if g != nil {
				_ = g.Close()
			}
		},
	}
}
