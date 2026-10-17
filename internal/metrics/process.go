package metrics

import (
	"os"
	"strconv"
	"strings"
	"time"
)

var startedAt = time.Now()

// ProcessStart is when this process began.
func ProcessStart() time.Time { return startedAt }

// ProcessStats are the resources the operating system attributes to the process.
type ProcessStats struct {
	// CPUSeconds is user plus system CPU time; zero when unavailable.
	CPUSeconds float64
	// RSSBytes is resident memory; zero when unavailable.
	RSSBytes uint64
}

// Process reads CPU time and resident memory (pure Go, no cgo).
func Process() ProcessStats {
	st := ProcessStats{CPUSeconds: cpuSeconds()}
	if b, err := os.ReadFile("/proc/self/statm"); err == nil {
		st.RSSBytes = parseStatm(string(b), uint64(os.Getpagesize()))
	}
	return st
}

// parseStatm returns the resident set from the content of /proc/self/statm.
func parseStatm(s string, page uint64) uint64 {
	f := strings.Fields(s)
	if len(f) < 2 {
		return 0
	}
	n, err := strconv.ParseUint(f[1], 10, 64)
	if err != nil {
		return 0
	}
	return n * page
}
