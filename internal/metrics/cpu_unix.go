//go:build !windows

package metrics

import "syscall"

func cpuSeconds() float64 {
	var ru syscall.Rusage
	_ = syscall.Getrusage(syscall.RUSAGE_SELF, &ru) // on failure the zero value reports no CPU time
	return tv(ru.Utime) + tv(ru.Stime)
}

func tv(t syscall.Timeval) float64 { return float64(t.Sec) + float64(t.Usec)/1e6 }
