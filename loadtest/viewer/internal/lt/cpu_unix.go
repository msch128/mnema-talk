//go:build !windows

package lt

import "syscall"

// processCPUSeconds is the CPU time (user plus system) this process has used.
func processCPUSeconds() float64 {
	var ru syscall.Rusage
	_ = syscall.Getrusage(syscall.RUSAGE_SELF, &ru) // on failure the zero value reports no CPU time
	return float64(ru.Utime.Sec) + float64(ru.Utime.Usec)/1e6 + float64(ru.Stime.Sec) + float64(ru.Stime.Usec)/1e6
}
