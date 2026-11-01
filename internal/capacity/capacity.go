// Package capacity turns what the server observes about itself (CPU, packet
// rates, members in calls) into an estimate of how many more members it can
// take, using the cost model measured in doc/benchmark.md. The result is an
// estimate for planning, never a promise: the model was fitted on one test host.
package capacity

import "math"

// The measured cost model (doc/benchmark.md, "The model").
const (
	// EgressCost is the CPU time of sending one packet to a member, in seconds.
	EgressCost = 22e-6
	// IngressCost is the CPU time of receiving one packet from a publisher.
	IngressCost = 72e-6
	// MemberCost is the CPU time per second of one connected member, in cores.
	MemberCost = 0.57e-3
	// VideoPacketBits is the measured average video packet, in bits.
	VideoPacketBits = 6500
	// ScreenBitrate is the bitrate of the screen share the estimate plans for.
	ScreenBitrate = 1.5e6
	// Speakers is the number of speakers in the planned call shape; each
	// sends 50 packets per second.
	Speakers = 3
	// PlanFraction of the cores is what the estimate plans to use: bursts lose
	// packets well before 100 % CPU.
	PlanFraction = 0.5
	// MinObservedPPS is the egress rate below which the host's own cost per
	// packet is not trusted and the table values are used.
	MinObservedPPS = 1000
	// MemoryTight is the share of the memory limit above which memory, not
	// CPU, is reported as the limit.
	MemoryTight = 0.85
)

// Basis says where the per-packet cost came from.
const (
	BasisTable    = "table"
	BasisObserved = "observed"
)

// Status says what limits the estimate.
const (
	StatusOK     = "ok"
	StatusMemory = "memory"
	StatusBusy   = "busy"
)

// Input is what the server measured, averaged over a short window.
type Input struct {
	// Cores is the CPU the server may use (cgroup quota or host cores).
	Cores float64
	// UsedCores is the process CPU use over the window.
	UsedCores float64
	// EgressPPS and IngressPPS are RTP packets per second over the window.
	EgressPPS, IngressPPS float64
	// Peers is the number of media connections.
	Peers int
	// RSSBytes and MemoryLimit; a zero limit means unknown.
	RSSBytes, MemoryLimit uint64
}

// Result is the estimate.
type Result struct {
	// MoreMembers is the number of further members, each watching one
	// screen share, the server can take with CPU to spare.
	MoreMembers int
	// Basis is BasisObserved when this host's own cost per packet was used.
	Basis string
	// Scale is observed CPU divided by the modelled CPU (1 for the table).
	Scale float64
	// PerMemberCores is the CPU one more member costs.
	PerMemberCores float64
	// PlanCores is the CPU the estimate allows itself to use.
	PlanCores float64
	// Status is StatusOK, StatusBusy (already past the plan) or StatusMemory.
	Status string
}

// modelled is the CPU the cost model predicts for the observed traffic.
func modelled(in Input) float64 {
	return in.EgressPPS*EgressCost + in.IngressPPS*IngressCost + float64(in.Peers)*MemberCost
}

// scale is how much more (or less) CPU this host spends than the model says,
// clamped so a noisy window cannot swing the estimate wildly.
func scale(in Input) (float64, string) {
	m := modelled(in)
	if in.EgressPPS < MinObservedPPS || m <= 0 || in.UsedCores <= 0 {
		return 1, BasisTable
	}
	return math.Min(4, math.Max(0.5, in.UsedCores/m)), BasisObserved
}

// MemberPackets is the packets per second one more member receives in the
// planned shape: the speakers' audio and one screen share.
func MemberPackets() float64 {
	return 50*Speakers + ScreenBitrate/VideoPacketBits
}

// Estimate computes how many more members the server can take.
func Estimate(in Input) Result {
	s, basis := scale(in)
	per := (MemberPackets()*EgressCost + MemberCost) * s
	plan := in.Cores * PlanFraction
	r := Result{Basis: basis, Scale: s, PerMemberCores: per, PlanCores: plan, Status: StatusOK}
	switch spare := plan - in.UsedCores; {
	case in.MemoryLimit > 0 && float64(in.RSSBytes) > MemoryTight*float64(in.MemoryLimit):
		r.Status = StatusMemory
	case spare <= 0:
		r.Status = StatusBusy
	default:
		r.MoreMembers = int(spare / per)
	}
	return r
}
