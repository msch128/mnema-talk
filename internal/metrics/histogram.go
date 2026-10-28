package metrics

import (
	"fmt"
	"io"
	"math"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

type histogram struct {
	counts [9]atomic.Uint64 // Buckets plus +Inf, non-cumulative
	sumNs  atomic.Uint64
	count  atomic.Uint64
}

// HistVec is a latency histogram family with one label.
type HistVec struct {
	name, help, label string
	max               int
	mu                sync.RWMutex
	series            map[string]*histogram
}

func newHistVec(name, help, label string) *HistVec {
	return &HistVec{name: name, help: help, label: label, max: maxSeries, series: map[string]*histogram{}}
}

func newHTTPHistVec(name, help, label string) *HistVec {
	h := newHistVec(name, help, label)
	h.max = maxHTTPSeries
	return h
}

func (h *HistVec) get(value string) *histogram {
	h.mu.RLock()
	s, ok := h.series[value]
	h.mu.RUnlock()
	if ok {
		return s
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if s, ok = h.series[value]; ok {
		return s
	}
	if len(h.series) >= h.max {
		value = otherLabel
		if s, ok = h.series[value]; ok {
			return s
		}
	}
	s = &histogram{}
	h.series[value] = s
	return s
}

// Observe records one duration under the label value.
func (h *HistVec) Observe(value string, d time.Duration) {
	s := h.get(value)
	s.counts[sort.SearchFloat64s(Buckets, d.Seconds())].Add(1)
	if d > 0 {
		s.sumNs.Add(uint64(d))
	}
	s.count.Add(1)
}

func (h *HistVec) write(w io.Writer) {
	h.mu.RLock()
	keys := make([]string, 0, len(h.series))
	for k := range h.series {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s histogram\n", h.name, h.help, h.name)
	for _, k := range keys {
		s := h.series[k]
		var cum uint64
		for i, le := range Buckets {
			cum += s.counts[i].Load()
			fmt.Fprintf(w, "%s_bucket{%s=%q,le=\"%s\"} %d\n", h.name, h.label, k, formatFloat(le), cum)
		}
		cum += s.counts[len(Buckets)].Load()
		fmt.Fprintf(w, "%s_bucket{%s=%q,le=\"+Inf\"} %d\n", h.name, h.label, k, cum)
		fmt.Fprintf(w, "%s_sum{%s=%q} %s\n", h.name, h.label, k, formatFloat(float64(s.sumNs.Load())/1e9))
		fmt.Fprintf(w, "%s_count{%s=%q} %d\n", h.name, h.label, k, s.count.Load())
	}
	h.mu.RUnlock()
	fmt.Fprintln(w)
}

func formatFloat(f float64) string {
	if math.IsInf(f, 0) || math.IsNaN(f) {
		return "0"
	}
	return fmt.Sprintf("%g", f)
}
