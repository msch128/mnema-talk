package turnrelay

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/pion/logging"
	"github.com/pion/turn/v5"
)

// CredentialTTL is how long minted credentials are valid. A browser keeps the
// credentials of a peer connection for its whole life and re-authenticates
// with them every few minutes, so this bounds the longest call over the
// relay; the server closes an allocation when its credential expires.
const CredentialTTL = 12 * time.Hour

// Realm is the TURN realm.
const Realm = "mnema-talk"

// RFC 5766 method numbers of the requests that start or extend an allocation.
const (
	methodAllocate = 0x003
	methodRefresh  = 0x004
)

// Start binds the relay. It fails only when it cannot listen at all or the
// SFU has no fixed media ports; a single transport that cannot bind is
// reported in Status and the other keeps running.
func Start(opts Options) (*Server, error) {
	if opts.Target == nil {
		return nil, errors.New("turn: no SFU target")
	}
	t := opts.Target()
	if !t.valid() {
		return nil, errors.New("turn: the SFU has no fixed UDP media port range to relay to (set WEBRTC_UDP_PORT_MIN/MAX)")
	}
	if opts.Port <= 0 || opts.Port > 65535 {
		return nil, fmt.Errorf("turn: invalid port %d", opts.Port)
	}
	if t.portOK(opts.Port) {
		return nil, fmt.Errorf("turn: port %d lies inside the SFU's media port range %d-%d", opts.Port, t.PortMin, t.PortMax)
	}
	auth, err := newAuthority()
	if err != nil {
		return nil, err
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	s := &Server{
		opts: opts, limits: opts.Limits.withDefaults(), auth: auth,
		guards: map[*guard]struct{}{}, byPort: map[int]*guard{}, byClient: map[string]*guard{}, perUser: map[uuid.UUID]int{},
	}
	s.allocLimit = newRateLimiter(s.limits.AllocationsPerMinute, s.limits.AllocationBurst, 4096)
	s.failLimit = newRateLimiter(s.limits.FailuresPerMinute, s.limits.FailureBurst, 4096)
	s.tgt.Store(&t)
	s.SetMayRelay(opts.MayRelay)
	s.probe.Store("unknown")
	s.status.Port = opts.Port

	if err := s.listen(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.stop = cancel
	s.wg.Add(1)
	go s.maintain(ctx)
	if opts.PublicHost != "" {
		s.wg.Add(1)
		go s.probeLoop(ctx)
	}
	return s, nil
}

func (s *Server) listen() error {
	addr := net.JoinHostPort(s.opts.BindAddr, strconv.Itoa(s.opts.Port))
	gen := &generator{s: s}
	cfg := turn.ServerConfig{
		Realm:         Realm,
		AuthHandler:   s.authenticate,
		QuotaHandler:  s.allowAllocation,
		EventHandler:  s.events(),
		LoggerFactory: &logging.DefaultLoggerFactory{Writer: io.Discard, DefaultLogLevel: logging.LogLevelDisabled},
		// Clients refresh before these run out; a client that went away
		// leaves nothing behind for long.
		AllocationLifetime: 10 * time.Minute,
		PermissionTimeout:  5 * time.Minute,
		ChannelBindTimeout: 10 * time.Minute,
	}
	var closers []io.Closer
	udp, udpErr := net.ListenPacket("udp4", addr)
	if udpErr == nil {
		cfg.PacketConnConfigs = []turn.PacketConnConfig{{PacketConn: udp, RelayAddressGenerator: gen, PermissionHandler: s.permit}}
		closers = append(closers, udp)
		s.status.UDP = true
	} else {
		s.status.UDPError = udpErr.Error()
	}
	tcp, tcpErr := net.Listen("tcp4", addr)
	if tcpErr == nil {
		s.tcp = newLimitedListener(tcp, s)
		cfg.ListenerConfigs = []turn.ListenerConfig{{Listener: s.tcp, RelayAddressGenerator: gen, PermissionHandler: s.permit}}
		closers = append(closers, tcp)
		s.status.TCP = true
	} else {
		s.status.TCPErr = tcpErr.Error()
	}
	if udpErr != nil && tcpErr != nil {
		return fmt.Errorf("turn: cannot listen on %s: %w", addr, errors.Join(udpErr, tcpErr))
	}
	srv, err := turn.NewServer(cfg)
	if err != nil {
		for _, c := range closers {
			_ = c.Close()
		}
		return fmt.Errorf("turn: %w", err)
	}
	s.turn = srv
	return nil
}

// SetMayRelay installs the check that a member may use the relay (they may
// join voice). The relay is started before the permission service exists, and
// refuses everyone until this is called.
func (s *Server) SetMayRelay(fn func(uuid.UUID) bool) {
	if fn == nil {
		s.SetRelayCheck(nil)
		return
	}
	s.SetRelayCheck(func(id uuid.UUID) (bool, error) { return fn(id), nil })
}

// SetRelayCheck is SetMayRelay for a check that can fail to tell.
func (s *Server) SetRelayCheck(fn RelayCheck) {
	if fn == nil {
		fn = func(uuid.UUID) (bool, error) { return false, nil }
	}
	s.check.Store(&fn)
}

// MayRelay reports whether the member may be handed credentials now.
func (s *Server) MayRelay(user uuid.UUID) bool {
	ok, err := (*s.check.Load())(user)
	return ok && err == nil
}

func (s *Server) now() time.Time { return s.opts.Now() }

func (s *Server) target() Target { return *s.tgt.Load() }

// refreshTarget re-reads the SFU's addresses; false when they no longer name
// a valid target, in which case the old ones stay.
func (s *Server) refreshTarget() {
	if t := s.opts.Target(); t.valid() {
		s.tgt.Store(&t)
	}
}

func (s *Server) debug(msg string, args ...any) { slog.Debug(msg, args...) }

// Credentials mints credentials for one member, bound to their identity and
// valid for CredentialTTL.
func (s *Server) Credentials(user uuid.UUID) (username, password string, expires time.Time) {
	return s.auth.mint(user, s.now(), CredentialTTL)
}

// VerifyCredential reports whether password is the one this relay minted for
// username (tests and diagnostics; the TURN exchange itself proves it through
// message integrity).
func (s *Server) VerifyCredential(username, password string) bool {
	return s.auth.verify(username, password)
}

// URLs lists the turn: URLs of the transports that are listening, for host.
func (s *Server) URLs(host string) []string {
	hostport := net.JoinHostPort(host, strconv.Itoa(s.opts.Port))
	var urls []string
	if s.status.UDP {
		urls = append(urls, "turn:"+hostport+"?transport=udp")
	}
	if s.status.TCP {
		urls = append(urls, "turn:"+hostport+"?transport=tcp")
	}
	return urls
}

// Status reports the listeners, the live allocations and the last probe.
func (s *Server) Status() Status {
	st := s.status
	s.mu.Lock()
	st.Allocations = len(s.guards)
	s.mu.Unlock()
	st.Probe, _ = s.probe.Load().(string)
	return st
}

// Close stops the relay and drops every allocation.
func (s *Server) Close() error {
	if !s.closed.CompareAndSwap(false, true) {
		return nil
	}
	s.stop()
	err := s.turn.Close()
	s.mu.Lock()
	open := make([]*guard, 0, len(s.guards))
	for g := range s.guards {
		open = append(open, g)
	}
	s.mu.Unlock()
	for _, g := range open {
		_ = g.Close()
	}
	s.wg.Wait()
	return err
}
