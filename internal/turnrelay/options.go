package turnrelay

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/pion/turn/v5"
)

// Limits bound what one member, one address and the relay as a whole may use.
// Zero fields take the defaults.
type Limits struct {
	// PerMember is the most allocations one member holds at once (a few
	// tabs or devices); Total is the most the relay holds.
	PerMember, Total int
	// AllocationsPerMinute and AllocationBurst rate-limit allocation attempts
	// per source address; FailuresPerMinute and FailureBurst the failed
	// authentications per source address, after which that address is
	// refused until its budget refills.
	AllocationsPerMinute, AllocationBurst int
	FailuresPerMinute, FailureBurst       int
	// BytesPerSecond and BurstBytes bound what a client may send through its
	// allocation to the SFU: far above any call, far below a flood.
	BytesPerSecond, BurstBytes int
	// IdleTimeout closes an allocation that forwards nothing for that long;
	// a live call never idles, its connectivity checks flow every few
	// seconds.
	IdleTimeout time.Duration
	// TCPPerAddress and TCPTotal bound the TCP connections to the relay.
	TCPPerAddress, TCPTotal int
}

func (l Limits) withDefaults() Limits {
	def := func(v *int, d int) {
		if *v <= 0 {
			*v = d
		}
	}
	def(&l.PerMember, 4)
	def(&l.Total, 256)
	// Sized for a whole office behind one address joining a Talk at once.
	def(&l.AllocationsPerMinute, 120)
	def(&l.AllocationBurst, 60)
	def(&l.FailuresPerMinute, 60)
	def(&l.FailureBurst, 50)
	def(&l.BytesPerSecond, 4<<20)
	def(&l.BurstBytes, 1<<20)
	def(&l.TCPPerAddress, 64)
	def(&l.TCPTotal, 1024)
	if l.IdleTimeout <= 0 {
		l.IdleTimeout = 2 * time.Minute
	}
	return l
}

// Options configure Start.
type Options struct {
	// Port is the UDP and TCP port to listen on.
	Port int
	// BindAddr is the address to listen on; empty means every interface.
	BindAddr string
	// Target reports where the SFU's media sockets are; it is read again
	// while running, so announced addresses may change.
	Target func() Target
	// MayRelay reports whether the member may use the relay right now: they
	// may join voice. Until it is set (here or with SetMayRelay) nobody may.
	MayRelay func(uuid.UUID) bool
	// PublicHost is the host name or address browsers reach the relay at,
	// for the reachability probe; empty skips the probe.
	PublicHost string
	Limits     Limits
	// Now replaces the clock (tests).
	Now func() time.Time
}

// Status describes the running relay.
type Status struct {
	Port int
	// UDP and TCP say which transports are listening; a bind failure on one
	// leaves the other running.
	UDP, TCP         bool
	UDPError, TCPErr string
	Allocations      int
	// Probe is the outcome of the self-test against PublicHost: "reachable",
	// "unreachable" or "unknown" (not run yet, or nothing to test).
	Probe string
}

// Server is the running relay.
type Server struct {
	opts   Options
	limits Limits
	auth   *authority
	turn   *turn.Server
	tgt    atomic.Pointer[Target]
	status Status

	allocLimit *rateLimiter
	failLimit  *rateLimiter

	mu       sync.Mutex
	guards   map[*guard]struct{}
	byPort   map[int]*guard
	byClient map[string]*guard
	perUser  map[uuid.UUID]int

	mayRelay atomic.Pointer[func(uuid.UUID) bool]

	probe  atomic.Value // string
	stop   context.CancelFunc
	wg     sync.WaitGroup
	closed atomic.Bool
}
