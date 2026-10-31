// Package memlimit gives the Go runtime a soft memory limit below the
// container's hard one. Without it the garbage collector lets the heap grow
// into the cgroup limit, the kernel then reclaims file pages (the binary's own
// code among them) and the process stalls or is killed; with it the collector
// works harder as the heap nears the limit instead.
package memlimit

import (
	"log/slog"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
)

// Percent of the cgroup limit the runtime is allowed to use.
const Percent = 85

// unlimitedV1 is the size above which a cgroup v1 limit means "none" (the
// kernel reports a page-rounded 2^63 for an unset limit).
const unlimitedV1 = 1 << 50

// Result says what Apply did.
type Result struct {
	// Source is "env" (GOMEMLIMIT was set and is left alone), "cgroup v2",
	// "cgroup v1" or "" when there is no limit to follow.
	Source string
	// Limit is the soft limit in bytes that Apply set (zero unless it set one).
	Limit int64
	// Container is the cgroup limit it was derived from.
	Container int64
}

// Apply sets the runtime's memory limit to Percent of the cgroup memory limit
// and logs what it chose. An explicit GOMEMLIMIT is never overridden.
func Apply(log *slog.Logger) Result {
	return apply("/sys/fs/cgroup", os.Getenv("GOMEMLIMIT"), debug.SetMemoryLimit, log)
}

func apply(root, env string, set func(int64) int64, log *slog.Logger) Result {
	if env != "" {
		log.Info("memory limit: GOMEMLIMIT is set, leaving it", "gomemlimit", env)
		return Result{Source: "env"}
	}
	source, container := cgroupLimit(root)
	if container <= 0 {
		log.Info("memory limit: no cgroup limit found, the Go runtime keeps its default")
		return Result{}
	}
	limit := container / 100 * Percent
	set(limit)
	log.Info("memory limit: set from the container limit",
		"source", source, "container_bytes", container, "gomemlimit_bytes", limit, "percent", Percent)
	return Result{Source: source, Limit: limit, Container: container}
}

// cgroupLimit reads the memory limit of this container: cgroup v2 first, then
// v1. It returns zero when there is none or it cannot be read.
func cgroupLimit(root string) (source string, limit int64) {
	if n, ok := readLimit(filepath.Join(root, "memory.max")); ok {
		return "cgroup v2", n
	}
	if n, ok := readLimit(filepath.Join(root, "memory", "memory.limit_in_bytes")); ok && n < unlimitedV1 {
		return "cgroup v1", n
	}
	return "", 0
}

// readLimit parses a limit file; "max" and unreadable files are no limit.
func readLimit(path string) (int64, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	n, err := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}
