package lt

import "time"

// CPUMeter measures how much CPU the load generator itself uses. A generator
// that runs out of CPU delays what it measures, so the report shows it: when
// it is close to the cores the host has, the latency is the generator's.
type CPUMeter struct {
	startCPU  float64
	startWall time.Time
}

// StartCPUMeter begins measuring.
func StartCPUMeter() *CPUMeter {
	return &CPUMeter{startCPU: processCPUSeconds(), startWall: time.Now()}
}

// Cores returns the average number of cores used since the start.
func (m *CPUMeter) Cores() float64 {
	wall := time.Since(m.startWall).Seconds()
	if wall <= 0 {
		return 0
	}
	return (processCPUSeconds() - m.startCPU) / wall
}
