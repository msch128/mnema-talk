// Package lt holds what the load-test tools share: configuration from the
// environment, session tokens, latency accounting, server metrics sampling
// and threshold checks.
package lt

import (
	"os"
	"strconv"
	"strings"
)

// Int reads an integer from the environment, def when unset or malformed.
func Int(key string, def int) int {
	if v, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key))); err == nil {
		return v
	}
	return def
}

// Float reads a number from the environment, def when unset or malformed.
func Float(key string, def float64) float64 {
	if v, err := strconv.ParseFloat(strings.TrimSpace(os.Getenv(key)), 64); err == nil {
		return v
	}
	return def
}

// Bool reads a flag: "1", "true" and "yes" are true.
func Bool(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes":
		return true
	}
	return false
}
