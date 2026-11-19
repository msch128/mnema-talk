//go:build linux

package fastudp

import (
	"errors"
	"net"
	"syscall"
	"testing"
)

// stubSendto replaces the system call for one test.
func stubSendto(t *testing.T, fn func(fd int, b []byte, ip [4]byte, port uint16) (int, bool, error)) {
	t.Helper()
	prev := sendto
	sendto = fn
	t.Cleanup(func() { sendto = prev })
}

// A send the kernel would block (a full send buffer) must still go out, through
// the runtime, which waits for room: under load this is the path that runs.
func TestFullSendBufferFallsBackToTheRuntime(t *testing.T) {
	r := newReceiver(t, "udp4", "127.0.0.1")
	w := Wrap(listen(t, "udp4", "127.0.0.1"))
	defer w.Close()
	calls := 0
	stubSendto(t, func(int, []byte, [4]byte, uint16) (int, bool, error) {
		calls++
		return 0, false, nil // what sendto4 returns for EAGAIN
	})
	payload := []byte("keyframe")
	n, err := w.WriteTo(payload, net.UDPAddrFromAddrPort(r.addr()))
	if err != nil || n != len(payload) {
		t.Fatalf("fallback write = %d, %v", n, err)
	}
	if calls != 1 {
		t.Fatalf("direct path tried %d times, want 1", calls)
	}
	if got := r.waitFor(1); got != 1 {
		t.Fatalf("%d of 1 datagram arrived through the fallback", got)
	}
}

func TestDirectSendErrorsAreReturned(t *testing.T) {
	r := newReceiver(t, "udp4", "127.0.0.1")
	w := Wrap(listen(t, "udp4", "127.0.0.1"))
	defer w.Close()
	stubSendto(t, func(int, []byte, [4]byte, uint16) (int, bool, error) {
		return 0, true, &net.OpError{Op: "write", Net: "udp", Err: syscall.EPERM}
	})
	_, err := w.WriteTo([]byte("x"), net.UDPAddrFromAddrPort(r.addr()))
	if !errors.Is(err, syscall.EPERM) {
		t.Fatalf("error = %v, want EPERM", err)
	}
}

func TestSendtoReportsWouldBlockAsNotDone(t *testing.T) {
	// A descriptor that cannot send: the real call reports an error, done.
	if _, done, err := sendto4(-1, []byte("x"), [4]byte{127, 0, 0, 1}, 9); !done || err == nil {
		t.Fatalf("bad descriptor: done=%v err=%v", done, err)
	}
	if _, done, _ := sendto4(-1, nil, [4]byte{127, 0, 0, 1}, 9); done {
		t.Fatal("an empty datagram must take the runtime's path")
	}
}
