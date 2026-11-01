package capacity

import (
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// Cores returns the CPU this process may use: the cgroup quota when there is
// one (cgroup v2 cpu.max, else v1 cpu.cfs_quota_us / cpu.cfs_period_us, under
// root), else the number of host cores.
func Cores(root string) float64 {
	host := float64(runtime.NumCPU())
	quota, period, ok := cpuQuota(root)
	if !ok || quota <= 0 || period <= 0 {
		return host
	}
	return math.Min(host, quota/period)
}

func cpuQuota(root string) (quota, period float64, ok bool) {
	if raw, err := os.ReadFile(filepath.Join(root, "cpu.max")); err == nil {
		f := strings.Fields(string(raw))
		if len(f) != 2 || f[0] == "max" {
			return 0, 0, false
		}
		return parseQuota(f[0], f[1])
	}
	q, err1 := os.ReadFile(filepath.Join(root, "cpu", "cpu.cfs_quota_us"))
	p, err2 := os.ReadFile(filepath.Join(root, "cpu", "cpu.cfs_period_us"))
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return parseQuota(strings.TrimSpace(string(q)), strings.TrimSpace(string(p)))
}

func parseQuota(q, p string) (float64, float64, bool) {
	quota, err1 := strconv.ParseFloat(q, 64)
	period, err2 := strconv.ParseFloat(p, 64)
	return quota, period, err1 == nil && err2 == nil
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
