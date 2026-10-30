package lt

import (
	"sync/atomic"
	"time"
)

// Histogram counts durations in fixed buckets, safe for concurrent use and
// without memory growth: 100 µs steps up to one second, then 10 ms steps up to
// 60 s, and one overflow bucket. A quantile is therefore exact to the bucket
// (100 µs below a second, 10 ms above), which is far below what a delivery
// latency target cares about.
type Histogram struct {
	buckets [fineBuckets + coarseBuckets + 1]atomic.Int64
	count   atomic.Int64
	sumUs   atomic.Int64
	maxUs   atomic.Int64
}

const (
	fineStep      = 100 * time.Microsecond
	fineBuckets   = 10_000 // up to 1 s
	coarseStep    = 10 * time.Millisecond
	coarseBuckets = 5_900 // 1 s .. 60 s
)

func bucketOf(d time.Duration) int {
	if d < 0 {
		d = 0
	}
	if d < fineBuckets*fineStep {
		return int(d / fineStep)
	}
	i := fineBuckets + int((d-fineBuckets*fineStep)/coarseStep)
	return min(i, fineBuckets+coarseBuckets)
}

// bucketUpper is the upper edge of bucket i, what a quantile reports.
func bucketUpper(i int) time.Duration {
	if i < fineBuckets {
		return time.Duration(i+1) * fineStep
	}
	return fineBuckets*fineStep + time.Duration(i-fineBuckets+1)*coarseStep
}

// Add records one duration.
func (h *Histogram) Add(d time.Duration) {
	h.buckets[bucketOf(d)].Add(1)
	h.count.Add(1)
	us := d.Microseconds()
	h.sumUs.Add(us)
	for {
		cur := h.maxUs.Load()
		if us <= cur || h.maxUs.CompareAndSwap(cur, us) {
			return
		}
	}
}

// Count is the number of recorded durations.
func (h *Histogram) Count() int64 { return h.count.Load() }

// Quantile returns the duration below which a fraction q (0..1) of the
// recorded values fall, zero when nothing was recorded.
func (h *Histogram) Quantile(q float64) time.Duration {
	total := h.count.Load()
	if total == 0 {
		return 0
	}
	// The rank is 1-based: the p99 of 100 values is the 99th.
	rank := int64(q*float64(total) + 0.999999)
	rank = min(max(rank, 1), total)
	var seen int64
	for i := range h.buckets {
		seen += h.buckets[i].Load()
		if seen >= rank {
			return bucketUpper(i)
		}
	}
	return time.Duration(h.maxUs.Load()) * time.Microsecond
}

// Summary is a histogram in milliseconds, for the JSON result.
type Summary struct {
	Count  int64   `json:"count"`
	P50Ms  float64 `json:"p50_ms"`
	P95Ms  float64 `json:"p95_ms"`
	P99Ms  float64 `json:"p99_ms"`
	MaxMs  float64 `json:"max_ms"`
	MeanMs float64 `json:"mean_ms"`
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

// Summarize reads the quantiles. The maximum is exact, not a bucket edge.
func (h *Histogram) Summarize() Summary {
	n := h.count.Load()
	s := Summary{Count: n}
	if n == 0 {
		return s
	}
	s.P50Ms, s.P95Ms, s.P99Ms = ms(h.Quantile(0.50)), ms(h.Quantile(0.95)), ms(h.Quantile(0.99))
	s.MaxMs = float64(h.maxUs.Load()) / 1000
	s.MeanMs = float64(h.sumUs.Load()) / float64(n) / 1000
	return s
}
