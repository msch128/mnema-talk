package capacity

import "math"

// The scenarios answer "how many people fit into one Talk used this way" on
// the CPU, memory and upload still free. Every figure below comes from
// doc/benchmark.md; scenarios that were never run as a whole are marked
// BasisDerived and built from the measured terms.

// Scenario IDs.
const (
	ScenarioChat         = "chat"
	ScenarioBroadcast    = "broadcast"
	ScenarioConversation = "conversation"
	ScenarioScreen       = "screen"
	ScenarioVideo        = "video"
)

// Basis of a scenario.
const (
	// BasisMeasured: the shape was run on the test host without loss.
	BasisMeasured = "measured"
	// BasisDerived: built from the measured costs, never run as a whole.
	BasisDerived = "derived"
)

// Limits: the resource that runs out first.
const (
	LimitCPU    = "cpu"
	LimitMemory = "memory"
	LimitUpload = "upload"
)

const (
	// AudioPPS is one continuous speaker: 50 packets/s (20 ms frames).
	AudioPPS = 50
	// AudioMbps is one speaker's audio as the upload formula counts it.
	AudioMbps = 0.04
	// UploadOverhead covers headers, keyframes and retransmissions (1.2 to 1.3 measured).
	UploadOverhead = 1.3
	// RSSPerHeap and the heap figures below follow the working rule
	// "1.3 times the heap plus 250 MB" (the 250 MB is already in the RSS now).
	RSSPerHeap = 1.3
	// CameraPublishBits is one camera sending all three layers at their caps
	// (2.5 Mbit/s, 500 and 150 kbit/s; web/src/lib/cameraLayers.ts).
	CameraPublishBits = 3.15e6
	// CameraViewBits is one camera as a grid tile receives it: the 360p layer.
	CameraViewBits = 500e3
	// MaxViewerCameras is the SFU's cap of cameras sent to one member.
	MaxViewerCameras = 24
	// chatLinear and chatSquare fit the chat runs (100 sessions 0.02 cores,
	// 1,000 sessions 0.67; 300 sessions predicted 0.09, measured 0.09).
	chatLinear = 1.478e-4
	chatSquare = 5.22e-7
	// chatSessionBytes is the resident memory per chat session (47 MB at
	// 100 sessions, 102 MB at 1,000).
	chatSessionBytes = 61e3
	// Peak live heap per member of the largest clean voice runs: 128 MB for
	// 3 speakers and 232 MB with a screen share at 100 members (2.4.0), 171 MB
	// with 10 cameras at 50 members (2.5.0).
	audioHeapPerMember  = 1.28e6
	screenHeapPerMember = 2.32e6
	cameraHeapPerMember = 3.42e6
	// maxSearch bounds the search for the largest fitting Talk.
	maxSearch = 1 << 20
)

// Load is what a Talk of some size costs.
type Load struct {
	// Cores is the CPU in cores.
	Cores float64
	// UploadMbps is the upload the server needs; MemoryBytes the resident memory.
	UploadMbps, MemoryBytes float64
}

// Scenario is one way of using a Talk.
type Scenario struct {
	ID    string
	Basis string
	// MeasuredUpTo is the largest Talk of this shape that ran cleanly (0: none).
	MeasuredUpTo int
	// Media is false for chat, whose CPU is not packet forwarding.
	Media bool
	Cost  func(n float64) Load
}

// mediaLoad turns packet rates and per-member heap into a Load.
func mediaLoad(n, ingress, egress, upload, heapPerMember float64) Load {
	return Load{
		Cores:       egress*EgressCost + ingress*IngressCost + n*MemberCost,
		UploadMbps:  UploadOverhead * upload,
		MemoryBytes: RSSPerHeap * heapPerMember * n,
	}
}

// Scenarios lists the scenarios the Status tab shows, in order.
func Scenarios() []Scenario {
	screenPPS := ScreenBitrate / VideoPacketBits
	return []Scenario{
		{ID: ScenarioChat, Basis: BasisMeasured, MeasuredUpTo: 1000, Cost: func(n float64) Load {
			return Load{Cores: chatLinear*n + chatSquare*n*n, MemoryBytes: chatSessionBytes * n}
		}},
		{ID: ScenarioBroadcast, Basis: BasisMeasured, MeasuredUpTo: 100, Media: true, Cost: func(n float64) Load {
			in := float64(AudioPPS * Speakers)
			return mediaLoad(n, in, (n-1)*in, (n-1)*Speakers*AudioMbps, audioHeapPerMember)
		}},
		{ID: ScenarioConversation, Basis: BasisDerived, Media: true, Cost: func(n float64) Load {
			return mediaLoad(n, AudioPPS*n, AudioPPS*n*(n-1), n*(n-1)*AudioMbps, audioHeapPerMember)
		}},
		{ID: ScenarioScreen, Basis: BasisMeasured, MeasuredUpTo: 100, Media: true, Cost: func(n float64) Load {
			in := AudioPPS*Speakers + screenPPS
			return mediaLoad(n, in, (n-1)*in, (n-1)*(Speakers*AudioMbps+ScreenBitrate/1e6), screenHeapPerMember)
		}},
		{ID: ScenarioVideo, Basis: BasisDerived, Media: true, Cost: videoCall},
	}
}

// videoCall: everyone talks with the camera on and sees up to 24 others.
func videoCall(n float64) Load {
	seen := math.Max(0, math.Min(n-1, MaxViewerCameras))
	in := n * (AudioPPS + CameraPublishBits/VideoPacketBits)
	out := n * ((n-1)*AudioPPS + seen*CameraViewBits/VideoPacketBits)
	return mediaLoad(n, in, out, n*((n-1)*AudioMbps+seen*CameraViewBits/1e6), cameraHeapPerMember)
}

// Budget is what is left for a new Talk.
type Budget struct {
	// Cores is the CPU left under the plan; Scale the host's media cost factor.
	Cores, Scale float64
	// MemoryBytes left before the memory switch; 0 when the limit is unknown.
	MemoryBytes float64
	// UploadMbps the server may send (VOICE_UPLOAD_LIMIT_MBIT); 0 when unknown.
	UploadMbps float64
}

// Fit is how many people one Talk of a scenario can hold.
type Fit struct {
	Members int
	// Limit is the resource the next member would exceed.
	Limit string
	// Load is the cost at Members on this host (media CPU scaled).
	Load Load
	// Extrapolated is true beyond the largest measured run, so always for a
	// derived scenario.
	Extrapolated bool
}

// cost is the load of n members on this host: media CPU scaled by the host.
func (b Budget) cost(s Scenario, n int) Load {
	l := s.Cost(float64(n))
	if s.Media {
		l.Cores *= b.Scale
	}
	return l
}

// over names the first resource a load exceeds, or "".
func (b Budget) over(l Load) string {
	switch {
	case l.Cores > b.Cores:
		return LimitCPU
	case b.MemoryBytes > 0 && l.MemoryBytes > b.MemoryBytes:
		return LimitMemory
	case b.UploadMbps > 0 && l.UploadMbps > b.UploadMbps:
		return LimitUpload
	}
	return ""
}

// FitScenario finds the largest Talk that fits the budget. All costs grow
// with the members, so a binary search over the size is exact.
func FitScenario(s Scenario, b Budget) Fit {
	lo, hi := 0, maxSearch
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if b.over(b.cost(s, mid)) == "" {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	f := Fit{Members: lo, Limit: b.over(b.cost(s, lo+1))}
	if f.Limit == "" {
		f.Limit = LimitCPU // the search bound; no real host gets here
	}
	if lo > 0 {
		f.Load = b.cost(s, lo)
	}
	f.Extrapolated = lo > s.MeasuredUpTo
	return f
}

// ScenarioFit is one row of the estimate.
type ScenarioFit struct {
	Scenario Scenario
	Fit      Fit
}

// FitAll fits every scenario into what is left of the plan.
func FitAll(in Input, plan, scale float64) []ScenarioFit {
	b := Budget{Cores: math.Max(0, plan-in.UsedCores), Scale: scale, UploadMbps: in.UploadLimitMbps}
	if in.MemoryLimit > 0 {
		b.MemoryBytes = math.Max(0, MemoryTight*float64(in.MemoryLimit)-float64(in.RSSBytes))
	}
	all := Scenarios()
	out := make([]ScenarioFit, len(all))
	for i, s := range all {
		out[i] = ScenarioFit{Scenario: s, Fit: FitScenario(s, b)}
	}
	return out
}
