package setupwizard

import (
	"context"
	"encoding/binary"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// fakeUDP answers every datagram with reply(request) until the test ends.
func fakeUDP(t *testing.T, reply func(req []byte) []byte) string {
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
			_, _ = pc.WriteTo(reply(buf[:n]), from)
		}
	}()
	return pc.LocalAddr().String()
}

// stunReply reports 203.0.113.7 as the caller's address (XOR-MAPPED-ADDRESS).
func stunReply(req []byte) []byte {
	out := make([]byte, 32)
	binary.BigEndian.PutUint16(out[0:], 0x0101)
	binary.BigEndian.PutUint16(out[2:], 12)
	copy(out[4:], req[4:20])
	binary.BigEndian.PutUint16(out[20:], 0x0020)
	binary.BigEndian.PutUint16(out[22:], 8)
	out[25] = 0x01
	ip := [4]byte{203, 0, 113, 7}
	cookie := [4]byte{0x21, 0x12, 0xA4, 0x42}
	for i := range ip {
		out[28+i] = ip[i] ^ cookie[i]
	}
	return out
}

// ntpReply answers with the current time.
func ntpReply([]byte) []byte {
	out := make([]byte, 48)
	out[0], out[1] = 0x1C, 2
	now := time.Now()
	binary.BigEndian.PutUint32(out[40:], uint32(now.Unix()+2208988800))
	return out
}

func TestRealProbeUsesTheNetwork(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	p := NewProbe([]string{fakeUDP(t, stunReply)}, []string{fakeUDP(t, ntpReply)})
	ctx := context.Background()

	if ip, err := p.PublicIP(ctx); err != nil || ip != netip.MustParseAddr("203.0.113.7") {
		t.Errorf("PublicIP = %v, %v", ip, err)
	}
	if skew, err := p.Skew(ctx); err != nil || skew > 2*time.Second || skew < -2*time.Second {
		t.Errorf("Skew = %v, %v", skew, err)
	}
	if addrs, err := p.Resolve(ctx, "localhost"); err != nil || len(addrs) == 0 {
		t.Errorf("Resolve(localhost) = %v, %v", addrs, err)
	}
	if err := p.Dial(ctx, ln.Addr().String()); err != nil {
		t.Errorf("Dial of a listening port: %v", err)
	}
	_ = ln.Close()
	if err := p.Dial(ctx, ln.Addr().String()); err == nil {
		t.Error("Dial of a closed port succeeded")
	}
}

func TestDefaultProbeIsComplete(t *testing.T) {
	p := DefaultProbe()
	if p.Resolve == nil || p.PublicIP == nil || p.Dial == nil || p.Skew == nil {
		t.Fatalf("%+v", p)
	}
}

func TestReadTemplateFindsTheGivenFileOrExplainsTheSearch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "compose.yml")
	if err := os.WriteFile(path, []byte("services: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if b, err := readTemplate(path); err != nil || string(b) != "services: {}\n" {
		t.Fatalf("readTemplate = %q, %v", b, err)
	}
	missing := filepath.Join(t.TempDir(), "nope.yml")
	if _, err := readTemplate(missing); err == nil || !strings.Contains(err.Error(), missing) || !strings.Contains(err.Error(), "--compose-template") {
		t.Fatalf("err = %v", err)
	}
}

func TestWriteExplainsAnUnwritableDirectory(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("directory permissions do not bind this user")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	_, err := Write(Options{Dir: dir, PublicURL: "https://chat.example.com"}, repoTemplate(t), NewSecrets(), fixedNow)
	if err == nil || !strings.Contains(err.Error(), "--user") {
		t.Fatalf("err = %v", err)
	}
}
