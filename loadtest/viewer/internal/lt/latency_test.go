package lt

import (
	"sync"
	"testing"
	"time"
)

func TestQuantilesOfAKnownDistribution(t *testing.T) {
	var h Histogram
	for i := 1; i <= 100; i++ {
		h.Add(time.Duration(i) * time.Millisecond)
	}
	s := h.Summarize()
	if s.Count != 100 {
		t.Fatalf("count = %d", s.Count)
	}
	// Buckets are 100 µs wide: a quantile may be reported up to one bucket high.
	for _, c := range []struct {
		name      string
		got, want float64
	}{{"p50", s.P50Ms, 50}, {"p95", s.P95Ms, 95}, {"p99", s.P99Ms, 99}, {"max", s.MaxMs, 100}} {
		if c.got < c.want || c.got > c.want+0.2 {
			t.Errorf("%s = %v ms, want %v", c.name, c.got, c.want)
		}
	}
	if s.MeanMs < 50.4 || s.MeanMs > 50.6 {
		t.Errorf("mean = %v ms, want 50.5", s.MeanMs)
	}
}

func TestOneSlowValueMovesOnlyTheTail(t *testing.T) {
	var h Histogram
	for range 999 {
		h.Add(2 * time.Millisecond)
	}
	h.Add(3 * time.Second)
	s := h.Summarize()
	if s.P99Ms > 3 || s.MaxMs < 3000 {
		t.Fatalf("summary = %+v", s)
	}
	if got := h.Quantile(1); got < 3*time.Second {
		t.Fatalf("p100 = %v", got)
	}
}

func TestLongAndNegativeDurationsStayInRange(t *testing.T) {
	var h Histogram
	h.Add(10 * time.Minute) // overflow bucket
	h.Add(-time.Second)     // clock step: counted as zero
	if h.Count() != 2 {
		t.Fatalf("count = %d", h.Count())
	}
	if q := h.Quantile(0.5); q > time.Millisecond {
		t.Fatalf("p50 = %v", q)
	}
	if q := h.Quantile(1); q < time.Minute {
		t.Fatalf("p100 = %v", q)
	}
}

func TestEmptyHistogramReportsZeros(t *testing.T) {
	var h Histogram
	if s := h.Summarize(); s != (Summary{}) {
		t.Fatalf("summary = %+v", s)
	}
}

func TestHistogramIsSafeForConcurrentUse(t *testing.T) {
	var h Histogram
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for i := range 1000 {
				h.Add(time.Duration(i) * time.Microsecond * 50)
			}
		})
	}
	wg.Wait()
	if h.Count() != 8000 {
		t.Fatalf("count = %d", h.Count())
	}
}
