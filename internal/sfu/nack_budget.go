package sfu

import (
	"sync"
	"time"
)

// A subscriber that loses packets asks for them again, and every resend is
// more load on the server and more traffic on a link that is already losing.
// Under overload that feeds itself: the retransmissions cause more loss, which
// causes more NACKs. So each subscriber connection has a retransmission
// budget, a token bucket: it fills at a share of what the connection is sent
// (and never less than a floor, so a quiet link still gets its keyframes
// repaired) and a resend that finds it empty is dropped and counted. The
// receiver recovers at the next keyframe, as it does for any packet that is
// no longer remembered.
const (
	// nackResendFloor is the least the budget fills at, in bytes per second.
	nackResendFloor = 128 << 10
	// nackResendShare is the share of the connection's video bitrate it fills at.
	nackResendShare = 0.25
	// nackResendBurst is how much time of the fill rate the bucket holds, so a
	// burst of loss (a keyframe's 40 packets) is repaired in one go.
	nackResendBurst = 500 * time.Millisecond
)

// resendLimits are the knobs of a subscriber's retransmission budget.
type resendLimits struct {
	floor float64
	share float64
	burst time.Duration
}

var defaultResendLimits = resendLimits{floor: nackResendFloor, share: nackResendShare, burst: nackResendBurst}

// rate is the fill rate for a connection sent mediaRate bytes per second.
func (l resendLimits) rate(mediaRate float64) float64 {
	return max(l.floor, l.share*mediaRate)
}

// resendBudget is the token bucket of one connection.
type resendBudget struct {
	mu     sync.Mutex
	limits resendLimits
	tokens float64
	at     time.Time
}

func newResendBudget(limits resendLimits) *resendBudget {
	return &resendBudget{limits: limits}
}

// allow takes size bytes from the bucket if it holds them. mediaRate is the
// bytes per second the connection is sent now.
func (b *resendBudget) allow(now time.Time, size int, mediaRate float64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	rate := b.limits.rate(mediaRate)
	capacity := rate * b.limits.burst.Seconds()
	if b.at.IsZero() {
		b.tokens = capacity
	} else if dt := now.Sub(b.at).Seconds(); dt > 0 {
		b.tokens = min(b.tokens+rate*dt, capacity)
	}
	b.at = now
	// A packet larger than the whole bucket still goes out once it is full.
	need := min(float64(size), capacity)
	if b.tokens < need {
		return false
	}
	b.tokens -= need
	return true
}
