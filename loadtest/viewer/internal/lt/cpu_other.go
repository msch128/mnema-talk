//go:build windows

package lt

// processCPUSeconds is not available on Windows: the generator is not measured.
func processCPUSeconds() float64 { return 0 }
