package server

import (
	"context"
	"runtime"
	"time"

	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/metrics"
	"github.com/msch128/mnema-talk/internal/sfu"
	"github.com/msch128/mnema-talk/internal/ws"
)

const (
	// opsInterval is the resolution of the System tab's charts and
	// opsHistory how many points are kept: 360 x 10 s = the last hour, at
	// roughly 100 bytes a point.
	opsInterval = 10 * time.Second
	opsHistory  = 360
)

// opsSource reads the live numbers behind /api/metrics and the System tab.
// Every part is optional.
type opsSource struct {
	db       *db.Pool
	hub      *ws.Hub
	sfu      *sfu.SFU
	storage  func() error // nil: not checked
	hasStore bool
	version  string
}

// opsReading is one point-in-time read of the process and its dependencies.
type opsReading struct {
	at       time.Time
	proc     metrics.ProcessStats
	mem      runtime.MemStats
	ws       ws.Stats
	sfu      sfu.Stats
	pool     db.PoolStats
	hasPool  bool
	storageU bool // object storage reachable
}

func (s *opsSource) read() opsReading {
	rd := opsReading{at: time.Now(), proc: metrics.Process()}
	runtime.ReadMemStats(&rd.mem)
	if s == nil {
		return rd
	}
	if s.hub != nil {
		rd.ws = s.hub.Stats()
	}
	if s.sfu != nil {
		rd.sfu = s.sfu.Stats()
	}
	if s.db != nil {
		rd.pool, rd.hasPool = s.db.Snapshot(), true
	}
	rd.storageU = s.hasStore && (s.storage == nil || s.storage() == nil)
	return rd
}

// OpsSample is one chart point (JSON of GET /api/admin/system/ops).
type OpsSample struct {
	// T is the unix time in seconds.
	T int64 `json:"t"`
	// CPUMillicores is process CPU use over the interval, 1000 = one core.
	CPUMillicores int64  `json:"cpu_millicores"`
	RSSBytes      uint64 `json:"rss_bytes"`
	HeapBytes     uint64 `json:"heap_bytes"`
	Goroutines    int    `json:"goroutines"`
	OnlineUsers   int    `json:"online_users"`
	Peers         int    `json:"peers"`
	ScreenShares  int    `json:"screen_shares"`
	EgressBps     uint64 `json:"egress_bps"`
	IngressBps    uint64 `json:"ingress_bps"`
	// EgressPPS and IngressPPS are RTP packets per second over the interval.
	EgressPPS     uint64 `json:"egress_pps"`
	IngressPPS    uint64 `json:"ingress_pps"`
	Requests      uint64 `json:"requests"`
	Errors5xx     uint64 `json:"errors_5xx"`
	ICEFailures   uint64 `json:"ice_failures"`
	DBAcquired    int32  `json:"db_connections_acquired"`
	WSConnections int    `json:"ws_connections"`
}

// opsCounters are the cumulative values the sampler differences.
type opsCounters struct {
	cpu                     float64
	reqs, e5xx, iceFail     uint64
	egressBytes, ingressByt uint64
	egressPkts, ingressPkts uint64
}

func readCounters(p metrics.ProcessStats) opsCounters {
	return opsCounters{
		cpu:         p.CPUSeconds,
		reqs:        metrics.HTTPRequests.Total(),
		e5xx:        metrics.HTTPRequests.Sum(1, "5xx"),
		iceFail:     metrics.SFUICE.Sum(0, "failed"),
		egressBytes: metrics.SFUEgressBytes.Load(),
		ingressByt:  metrics.SFUIngressBytes.Load(),
		egressPkts:  metrics.SFUEgressPackets.Load(),
		ingressPkts: metrics.SFUIngressPackets.Load(),
	}
}

// opsSampler keeps the last opsHistory samples in a ring buffer.
type opsSampler struct {
	src  *opsSource
	ring *metrics.Ring[OpsSample]
	// every is the sampling period (opsInterval; tests shorten it).
	every time.Duration

	lastAt  time.Time
	lastCtr opsCounters
}

func newOpsSampler(src *opsSource) *opsSampler {
	return &opsSampler{src: src, ring: metrics.NewRing[OpsSample](opsHistory), every: opsInterval}
}

// sample takes one reading and appends it to the ring. The first call only
// establishes the baseline for the rates.
func (o *opsSampler) sample() {
	rd := o.src.read()
	ctr := readCounters(rd.proc)
	if !o.lastAt.IsZero() {
		dt := rd.at.Sub(o.lastAt).Seconds()
		if dt > 0 {
			sm := OpsSample{
				T:             rd.at.Unix(),
				CPUMillicores: int64(max(0, (ctr.cpu-o.lastCtr.cpu)/dt*1000)),
				RSSBytes:      rd.proc.RSSBytes,
				HeapBytes:     rd.mem.HeapAlloc,
				Goroutines:    runtime.NumGoroutine(),
				OnlineUsers:   rd.ws.OnlineUsers,
				WSConnections: rd.ws.Connections,
				Peers:         rd.sfu.Peers,
				ScreenShares:  rd.sfu.ScreenShares,
				EgressBps:     uint64(float64(ctr.egressBytes-o.lastCtr.egressBytes) * 8 / dt),
				IngressBps:    uint64(float64(ctr.ingressByt-o.lastCtr.ingressByt) * 8 / dt),
				EgressPPS:     uint64(float64(ctr.egressPkts-o.lastCtr.egressPkts) / dt),
				IngressPPS:    uint64(float64(ctr.ingressPkts-o.lastCtr.ingressPkts) / dt),
				Requests:      ctr.reqs - o.lastCtr.reqs,
				Errors5xx:     ctr.e5xx - o.lastCtr.e5xx,
				ICEFailures:   ctr.iceFail - o.lastCtr.iceFail,
			}
			if rd.hasPool {
				sm.DBAcquired = rd.pool.Acquired
			}
			o.ring.Push(sm)
		}
	}
	o.lastAt, o.lastCtr = rd.at, ctr
}

// run samples until ctx ends.
func (o *opsSampler) run(ctx context.Context) {
	o.sample()
	t := time.NewTicker(o.every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			o.sample()
		}
	}
}
