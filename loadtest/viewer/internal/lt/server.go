package lt

import "time"

// ServerSummary is what the server did during the loaded phases, from the
// metric samples. CPU is in cores (CPU seconds per wall second); the process
// counter of the server's own container, not of the machine.
type ServerSummary struct {
	Measured     bool    `json:"measured"`
	Samples      int     `json:"samples"`
	ScrapeErrors int     `json:"scrape_errors,omitempty"`
	CPUCoresAvg  float64 `json:"cpu_cores_avg"`
	// CPUCoresHold is the average over the steady "hold" phase alone.
	CPUCoresHold   float64 `json:"cpu_cores_hold"`
	CPUCoresPeak   float64 `json:"cpu_cores_peak"`
	RSSPeakMB      float64 `json:"rss_peak_mb"`
	HeapPeakMB     float64 `json:"heap_peak_mb"`
	GoroutinesPre  float64 `json:"goroutines_pre"`
	GoroutinesPeak float64 `json:"goroutines_peak"`
	GoroutinesPost float64 `json:"goroutines_post"`
	// Deltas are the growth of the counters between the first and last
	// loaded sample, by metric name.
	Deltas map[string]float64 `json:"counter_deltas,omitempty"`
	// RunDeltas are the growth between the first and the last sample of the
	// whole run, for balancing packets sent against packets received.
	RunDeltas map[string]float64 `json:"run_counter_deltas,omitempty"`
}

// loadedPhase says whether a phase counts as under load.
func loadedPhase(p string) bool { return p == "hold" || p == "ramp" }

const mb = 1 << 20

// peakWindow is the shortest span a CPU peak is judged over: scrapes are
// seconds apart and a single short interval says little.
const peakWindow = 10 * time.Second

// SummarizeServer reduces the samples. phases "pre" and "post" are the
// baseline before and after; "ramp" and "hold" are loaded.
func SummarizeServer(samples []Sample, scrapeErrors int) ServerSummary {
	s := ServerSummary{Samples: len(samples), ScrapeErrors: scrapeErrors, Measured: len(samples) > 0}
	if !s.Measured {
		return s
	}
	var loaded, hold []Sample
	for _, smp := range samples {
		s.RSSPeakMB = max(s.RSSPeakMB, smp.Values["process_resident_memory_bytes"]/mb)
		s.HeapPeakMB = max(s.HeapPeakMB, smp.Values["go_memstats_alloc_bytes"]/mb)
		s.GoroutinesPeak = max(s.GoroutinesPeak, smp.Values["go_goroutines"])
		if loadedPhase(smp.Phase) {
			loaded = append(loaded, smp)
		}
		if smp.Phase == "hold" {
			hold = append(hold, smp)
		}
	}
	s.GoroutinesPre = firstOf(samples, "pre", "go_goroutines", false)
	s.GoroutinesPost = firstOf(samples, "post", "go_goroutines", true)
	if len(loaded) >= 2 {
		s.CPUCoresAvg, s.CPUCoresPeak = cpuCores(loaded)
		s.Deltas = CounterDeltas(loaded[0], loaded[len(loaded)-1])
	}
	if len(samples) >= 2 {
		s.RunDeltas = CounterDeltas(samples[0], samples[len(samples)-1])
	}
	if len(hold) >= 2 {
		s.CPUCoresHold, _ = cpuCores(hold)
	}
	return s
}

// firstOf returns the value of name in the first (or last) sample of a phase.
func firstOf(samples []Sample, phase, name string, last bool) float64 {
	var v float64
	for _, smp := range samples {
		if smp.Phase != phase {
			continue
		}
		v = smp.Values[name]
		if !last {
			return v
		}
	}
	return v
}

// cpuCores returns the average over the samples and the highest average over
// a window of at least peakWindow.
func cpuCores(loaded []Sample) (avg, peak float64) {
	const key = "process_cpu_seconds_total"
	first, last := loaded[0], loaded[len(loaded)-1]
	if wall := last.At.Sub(first.At).Seconds(); wall > 0 {
		avg = (last.Values[key] - first.Values[key]) / wall
	}
	for i := range loaded {
		for j := i + 1; j < len(loaded); j++ {
			wall := loaded[j].At.Sub(loaded[i].At)
			if wall < peakWindow {
				continue
			}
			peak = max(peak, (loaded[j].Values[key]-loaded[i].Values[key])/wall.Seconds())
			break
		}
	}
	return avg, max(peak, avg)
}

// CounterDeltas returns how far the counters grew between two samples.
func CounterDeltas(a, b Sample) map[string]float64 {
	out := map[string]float64{}
	for name, v := range b.Values {
		if !isCounter(name) {
			continue
		}
		if d := v - a.Values[name]; d > 0 {
			out[name] = d
		}
	}
	return out
}

func isCounter(name string) bool {
	return len(name) > 6 && name[len(name)-6:] == "_total" && name != "process_cpu_seconds_total"
}
