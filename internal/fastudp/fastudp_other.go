//go:build !linux

package fastudp

import "net"

// rawFD: writes take the wrapped conn on platforms other than Linux.
func rawFD(*net.UDPConn) (int, bool) { return -1, false }

func sendto4(int, []byte, [4]byte, uint16) (int, bool, error) { return 0, false, nil }
