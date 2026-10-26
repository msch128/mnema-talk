//go:build unix

package media

import "syscall"

// freeDiskBytes is the space available to unprivileged writers at path.
func freeDiskBytes(path string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, err
	}
	return uint64(st.Bavail) * uint64(st.Bsize), nil //nolint:gosec // block counts are not negative
}

// FreeDiskBytes is the space available to unprivileged writers at path.
func FreeDiskBytes(path string) (uint64, error) {
	return freeDiskBytes(path)
}
