package capacity

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// Cores returns the CPU this process may use: the cgroup quota when there is
// one (cgroup v2 cpu.max under root), else the number of host cores.
func Cores(root string) float64 {
	host := float64(runtime.NumCPU())
	raw, err := os.ReadFile(filepath.Join(root, "cpu.max"))
	if err != nil {
		return host
	}
	fields := strings.Fields(string(raw))
	if len(fields) != 2 || fields[0] == "max" {
		return host
	}
	quota, err1 := strconv.ParseFloat(fields[0], 64)
	period, err2 := strconv.ParseFloat(fields[1], 64)
	if err1 != nil || err2 != nil || quota <= 0 || period <= 0 {
		return host
	}
	if c := quota / period; c < host {
		return c
	}
	return host
}

// MemoryLimit returns the container's memory limit in bytes from the cgroup
// under root (v2 memory.max, then v1), or zero when there is none.
func MemoryLimit(root string) uint64 {
	for _, p := range []string{"memory.max", filepath.Join("memory", "memory.limit_in_bytes")} {
		raw, err := os.ReadFile(filepath.Join(root, p))
		if err != nil {
			continue
		}
		n, err := strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 64)
		if err == nil && n > 0 && n < 1<<60 {
			return n
		}
	}
	return 0
}
