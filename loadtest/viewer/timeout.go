package main

import (
	"fmt"
	"time"
)

// iceTimeout is how long a member may take to connect (ICE_TIMEOUT_S).
func iceTimeout() time.Duration {
	return time.Duration(envInt("ICE_TIMEOUT_S", 25)) * time.Second
}

// watchConnect gives a member d to connect. A member that does not is marked
// failed with its ICE diagnostics and closed, so the run keeps going and the
// cause is in the result instead of a silent "never connected".
func (m *member) watchConnect(d time.Duration) {
	select {
	case <-time.After(d):
	case <-m.stop:
		return
	}
	if m.everConn.Load() {
		return
	}
	m.timedOut.Store(true)
	m.event("ice connect timeout after %s", d)
	m.diagnose()
	m.failed.CompareAndSwap(nil, fmt.Sprintf("ice connect timeout after %s", d))
	go m.close()
}
