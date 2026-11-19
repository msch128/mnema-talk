//go:build linux

package fastudp

import (
	"errors"
	"net"
	"os"
	"syscall"
	"unsafe"
)

// rawFD returns the descriptor of an IPv4 socket. IPv6 and dual-stack
// sockets keep the wrapped conn's path: their destinations need the v4-mapped
// form and zones, which the runtime already handles.
func rawFD(c *net.UDPConn) (int, bool) {
	sc, err := c.SyscallConn()
	if err != nil {
		return -1, false
	}
	fd := -1
	if err := sc.Control(func(f uintptr) { fd = int(f) }); err != nil || fd < 0 {
		return -1, false
	}
	sa, err := syscall.Getsockname(fd)
	if err != nil {
		return -1, false
	}
	if _, ok := sa.(*syscall.SockaddrInet4); !ok {
		return -1, false
	}
	return fd, true
}

// sendto4 sends one datagram with sendto(2). The socket is non-blocking (the
// runtime made it so): when the kernel would block, done is false and the
// caller waits for the socket through the runtime instead.
func sendto4(fd int, b []byte, ip [4]byte, port uint16) (n int, done bool, err error) {
	if len(b) == 0 {
		return 0, false, nil
	}
	var sa syscall.RawSockaddrInet4
	sa.Family = syscall.AF_INET
	p := (*[2]byte)(unsafe.Pointer(&sa.Port))
	p[0], p[1] = byte(port>>8), byte(port)
	sa.Addr = ip
	for {
		r, _, errno := syscall.Syscall6(syscall.SYS_SENDTO, uintptr(fd),
			uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)), 0,
			uintptr(unsafe.Pointer(&sa)), unsafe.Sizeof(sa))
		switch {
		case errno == 0:
			return int(r), true, nil
		case errno == syscall.EINTR:
			continue
		case errors.Is(errno, syscall.EAGAIN):
			return 0, false, nil
		}
		return 0, true, &net.OpError{Op: "write", Net: "udp", Err: os.NewSyscallError("sendto", errno)}
	}
}
