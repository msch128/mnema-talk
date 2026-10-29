package turnrelay

import (
	"net"
	"time"

	"github.com/google/uuid"

	"github.com/msch128/mnema-talk/internal/metrics"
	"github.com/pion/turn/v5"
)

// authenticate is the TURN auth handler. Pion calls it before it verifies
// the request's message integrity, so it does nothing a forger could abuse:
// parse the username, check its expiry and derive the key. Whether the member
// may still join voice is decided later, once the request is proven genuine
// (generator.AllocatePacketConn for a new allocation, OnAuth for running ones).
func (s *Server) authenticate(ra *turn.RequestAttributes) (string, []byte, bool) {
	now := s.now()
	ip := hostOf(ra.SrcAddr)
	// Only malformed and expired usernames count as failures, per source
	// address and username. A UDP source can be spoofed, and a budget per
	// address alone would let anyone lock a real user (or a whole office behind
	// one NAT) out with forged requests; a forged request that names a valid
	// username is not counted here for the same reason, it fails the integrity
	// check later and costs nothing but that check.
	key := ip + "|" + ra.Username
	if !s.failLimit.available(key, now) {
		metrics.TURNEvents.With("rate_limited").Inc()
		return "", nil, false
	}
	if _, _, err := s.auth.check(ra.Username, now); err != nil {
		s.failLimit.allow(key, now)
		metrics.TURNEvents.With("auth_failed").Inc()
		s.debug("turn: refused credentials", "from", ip, "err", err)
		return "", nil, false
	}
	// The credential's username doubles as the allocation's identity: it
	// carries the member and the expiry the generator needs.
	return ra.Username, turn.GenerateAuthKey(ra.Username, ra.Realm, s.auth.password(ra.Username)), true
}

// verdictTTL is how long the answer to "may this member join voice" is reused.
const verdictTTL = 8 * time.Second

type verdict struct {
	ok bool
	at time.Time
}

// flight is a lookup in progress, shared by everyone who asks about the member
// meanwhile.
type flight struct {
	done chan struct{}
	ok   bool
	err  error
}

// lookup asks the permission check about a member: one lookup per member at a
// time, and a definite answer is reused for verdictTTL. An error is returned,
// not cached.
func (s *Server) lookup(user uuid.UUID) (bool, error) {
	now := s.now()
	s.mu.Lock()
	if v, ok := s.verdicts[user]; ok && now.Sub(v.at) < verdictTTL {
		s.mu.Unlock()
		return v.ok, nil
	}
	if f, ok := s.flights[user]; ok {
		s.mu.Unlock()
		<-f.done
		return f.ok, f.err
	}
	f := &flight{done: make(chan struct{})}
	s.flights[user] = f
	s.mu.Unlock()

	f.ok, f.err = (*s.check.Load())(user)

	s.mu.Lock()
	delete(s.flights, user)
	if f.err == nil {
		if len(s.verdicts) > 4096 {
			clear(s.verdicts)
		}
		s.verdicts[user] = verdict{ok: f.ok, at: s.now()}
	}
	s.mu.Unlock()
	close(f.done)
	return f.ok, f.err
}

// mayJoin is the answer for a new allocation, whose request passed the
// integrity check. A definite no also ends the member's running allocations.
func (s *Server) mayJoin(user uuid.UUID) (bool, error) {
	ok, err := s.lookup(user)
	if err == nil && !ok {
		s.DropMember(user)
	}
	return ok, err
}

// revalidate re-checks a member whose request just passed the integrity check:
// a definite "no" ends their allocations, an error keeps them. It runs in the
// background so it never blocks the server loop; concurrent asks share one
// lookup with mayJoin.
func (s *Server) revalidate(user uuid.UUID) {
	go func() {
		if ok, err := s.mayJoin(user); err == nil && !ok {
			metrics.TURNEvents.With("forbidden").Inc()
			s.debug("turn: member may no longer join voice, closing", "user", user)
		}
	}()
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
		// Pion reports a verified request with verdict true (the message
		// integrity matched): the first moment a lookup may be spent on it.
		OnAuth: func(_, _ net.Addr, _, username, _, _ string, verdict bool) {
			if !verdict {
				return
			}
			if user, _, err := parseUsername(username); err == nil {
				s.revalidate(user)
			}
		},
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
