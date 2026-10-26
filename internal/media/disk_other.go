//go:build !unix

package media

import "errors"

// freeDiskBytes is not available here; the free-space guard stays off.
func freeDiskBytes(string) (uint64, error) {
	return 0, errors.New("free disk space is not available on this platform")
}

// FreeDiskBytes is not available here; the free-space guard stays off.
func FreeDiskBytes(path string) (uint64, error) {
	return freeDiskBytes(path)
}
