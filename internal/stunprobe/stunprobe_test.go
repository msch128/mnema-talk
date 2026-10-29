package stunprobe

import (
	"context"
	"encoding/binary"
	"net"
	"net/netip"
	"testing"
	"time"
)

// fakeSTUN answers binding requests with the given reply builder until the
// test ends. It returns the address to dial.
func fakeSTUN(t *testing.T, reply func(req []byte) []byte) string {
	t.Helper()
	pc, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	go func() {
		buf := make([]byte, 1500)
		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			if out := reply(buf[:n]); out != nil {
				_, _ = pc.WriteTo(out, from)
			}
		}
	}()
	return pc.LocalAddr().String()
}

// successResponse builds a binding success response for req reporting ip in
// an XOR-MAPPED-ADDRESS attribute.
func successResponse(req []byte, ip netip.Addr, xor bool) []byte {
	attr := make([]byte, 12)
	typ := uint16(attrMappedAddress)
	if xor {
		typ = attrXORMapped
	}
	binary.BigEndian.PutUint16(attr[0:], typ)
	binary.BigEndian.PutUint16(attr[2:], 8)
	attr[5] = familyIPv4
	binary.BigEndian.PutUint16(attr[6:], 4242)
	raw := ip.As4()
	if xor {
		var m [4]byte
		binary.BigEndian.PutUint32(m[:], magicCookie)
		for i := range raw {
			raw[i] ^= m[i]
		}
	}
	copy(attr[8:], raw[:])
	out := make([]byte, headerLen, headerLen+len(attr))
	binary.BigEndian.PutUint16(out[0:], typeBindingSuccess)
	binary.BigEndian.PutUint16(out[2:], uint16(len(attr)))
	copy(out[4:], req[4:20])
	return append(out, attr...)
}

func TestDetectXORMappedAddress(t *testing.T) {
	want := netip.MustParseAddr("203.0.113.7")
	srv := fakeSTUN(t, func(req []byte) []byte { return successResponse(req, want, true) })
	got, via, err := Detect(context.Background(), []string{srv}, time.Second)
	if err != nil || got != want || via != srv {
		t.Fatalf("Detect = %v, %q, %v; want %v via %s", got, via, err, want, srv)
	}
}

func TestDetectPlainMappedAddress(t *testing.T) {
	want := netip.MustParseAddr("198.51.100.20")
	srv := fakeSTUN(t, func(req []byte) []byte { return successResponse(req, want, false) })
	got, _, err := Detect(context.Background(), []string{"stun:" + srv}, time.Second)
	if err != nil || got != want {
		t.Fatalf("Detect = %v, %v; want %v", got, err, want)
	}
}

func TestDetectFallsBackToTheNextServer(t *testing.T) {
	want := netip.MustParseAddr("203.0.113.9")
	silent := fakeSTUN(t, func([]byte) []byte { return nil })
	good := fakeSTUN(t, func(req []byte) []byte { return successResponse(req, want, true) })
	got, via, err := Detect(context.Background(), []string{silent, good}, 300*time.Millisecond)
	if err != nil || got != want || via != good {
		t.Fatalf("Detect = %v, %q, %v; want %v via %s", got, via, err, want, good)
	}
}

func TestDetectRetriesALostPacket(t *testing.T) {
	want := netip.MustParseAddr("203.0.113.11")
	calls := 0
	srv := fakeSTUN(t, func(req []byte) []byte {
		calls++
		if calls == 1 {
			return nil
		}
		return successResponse(req, want, true)
	})
	got, _, err := Detect(context.Background(), []string{srv}, 900*time.Millisecond)
	if err != nil || got != want {
		t.Fatalf("Detect = %v, %v; want %v", got, err, want)
	}
}

func TestDetectRejectsPrivateAndForeignAnswers(t *testing.T) {
	private := fakeSTUN(t, func(req []byte) []byte {
		return successResponse(req, netip.MustParseAddr("192.168.1.5"), true)
	})
	foreign := fakeSTUN(t, func(req []byte) []byte {
		bad := append([]byte(nil), req...)
		bad[10] ^= 0xff // another transaction
		return successResponse(bad, netip.MustParseAddr("203.0.113.7"), true)
	})
	for name, srv := range map[string]string{"private": private, "foreign transaction": foreign} {
		if got, _, err := Detect(context.Background(), []string{srv}, 300*time.Millisecond); err == nil {
			t.Errorf("%s: Detect = %v, want an error", name, got)
		}
	}
}

func TestDetectWithoutServers(t *testing.T) {
	if _, _, err := Detect(context.Background(), nil, time.Second); err == nil {
		t.Fatal("want an error without servers")
	}
}

func TestHostPort(t *testing.T) {
	for in, want := range map[string]string{
		"stun.example.org":           "stun.example.org:3478",
		"stun:stun.example.org":      "stun.example.org:3478",
		"stun:stun.example.org:1234": "stun.example.org:1234",
		"stun.example.org:19302":     "stun.example.org:19302",
		"[2001:db8::1]":              "[2001:db8::1]:3478",
	} {
		if got := hostPort(in); got != want {
			t.Errorf("hostPort(%q) = %q, want %q", in, got, want)
		}
	}
}
