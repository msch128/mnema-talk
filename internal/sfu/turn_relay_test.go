package sfu

import (
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/metrics"
	"github.com/msch128/mnema-talk/internal/turnrelay"
	"github.com/pion/webrtc/v4"
)

// freeUDPPort returns a UDP port nobody listens on.
func freeUDPPort(t *testing.T) uint16 {
	t.Helper()
	c, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	return uint16(c.LocalAddr().(*net.UDPAddr).Port)
}

// freeUDPRange returns the first of n consecutive UDP ports that are all free
// and cannot overflow a uint16.
func freeUDPRange(t *testing.T, n int) uint16 {
	t.Helper()
	for range 50 {
		first := freeUDPPort(t)
		if int(first)+n > 65000 {
			continue
		}
		var held []net.PacketConn
		ok := true
		for i := range n {
			c, err := net.ListenPacket("udp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(int(first)+i)))
			if err != nil {
				ok = false
				break
			}
			held = append(held, c)
		}
		for _, c := range held {
			_ = c.Close()
		}
		if ok {
			return first
		}
	}
	t.Fatal("no free UDP port range")
	return 0
}

// freeRelayPort returns a port free for both UDP and TCP.
func freeRelayPort(t *testing.T) int {
	t.Helper()
	for range 50 {
		port := int(freeUDPPort(t))
		l, err := net.Listen("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if err == nil {
			_ = l.Close()
			return port
		}
	}
	t.Fatal("no port free for UDP and TCP")
	return 0
}

// relayFor starts the built-in relay for s the way main does.
func relayFor(t *testing.T, s *SFU) *turnrelay.Server {
	t.Helper()
	relay, err := turnrelay.Start(turnrelay.Options{
		Port:     freeRelayPort(t),
		BindAddr: "127.0.0.1",
		Target: func() turnrelay.Target {
			ep := s.Endpoints()
			return turnrelay.Target{LocalIPs: ep.Local, Announced: ep.Announced, PortMin: ep.PortMin, PortMax: ep.PortMax}
		},
		MayRelay: func(uuid.UUID) bool { return true },
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = relay.Close() })
	return relay
}

// newRelayClient is a browser stand-in that may only use the TURN relay, like
// one whose network drops all direct UDP, signed in as a fresh member.
func newRelayClient(t *testing.T, relay *turnrelay.Server, transport string) *client {
	t.Helper()
	user := uuid.New()
	name, secret, _ := relay.Credentials(user)
	var url string
	for _, u := range relay.URLs("127.0.0.1") {
		if strings.HasSuffix(u, "transport="+transport) {
			url = u
		}
	}
	if url == "" {
		t.Fatalf("relay has no %s URL: %v", transport, relay.URLs("127.0.0.1"))
	}
	c := newClientConfig(t, func(se *webrtc.SettingEngine) {
		se.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4, webrtc.NetworkTypeTCP4})
	}, webrtc.Configuration{
		ICEServers:         []webrtc.ICEServer{{URLs: []string{url}, Username: name, Credential: secret}},
		ICETransportPolicy: webrtc.ICETransportPolicyRelay,
	})
	c.id = user
	return c
}

// Two members that can only use the relay still hear each other: media goes
// browser -> relay -> SFU -> relay -> browser. It holds for the shared UDP port
// and the per-peer port range, announced loopback or a public address the
// host cannot reach itself, and for TURN over UDP and over TCP.
func TestRelayOnlyClientsExchangeAudio(t *testing.T) {
	nonLoopback := false
	for _, ip := range interfaceIPv4s(false, false) {
		nonLoopback = nonLoopback || !ip.IsLoopback()
	}
	cases := []struct {
		name      string
		announce  []string
		mux       bool
		transport string
		needsNIC  bool
	}{
		{"loopback shared port over udp", []string{"127.0.0.1"}, true, "udp", false},
		{"loopback shared port over tcp", []string{"127.0.0.1"}, true, "tcp", false},
		{"loopback port range", []string{"127.0.0.1"}, false, "udp", false},
		{"nat 1:1 public address shared port", []string{"203.0.113.7"}, true, "udp", true},
		{"nat 1:1 public address over tcp", []string{"203.0.113.7"}, true, "tcp", true},
		{"nat 1:1 public and lan address", []string{"203.0.113.7", "192.0.2.9"}, true, "udp", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.needsNIC && !nonLoopback {
				t.Skip("needs a non-loopback IPv4 interface")
			}
			var s *SFU
			var err error
			if tc.mux {
				port := freeUDPPort(t)
				s, err = NewSFU(port, port, tc.announce, nil, WithUDPMuxPort(port))
			} else {
				min := freeUDPRange(t, 21)
				s, err = NewSFU(min, min+20, tc.announce, nil)
			}
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = s.Close() })
			relay := relayFor(t, s)
			before := metrics.TURNRelayedBytes.Sum(0, "to_sfu")

			room := uuid.New()
			a := newRelayClient(t, relay, tc.transport)
			a.publishAudio("mic")
			a.join(s, room)
			b := newRelayClient(t, relay, tc.transport)
			b.publishAudio("mic")
			b.join(s, room)

			for name, c := range map[string]*client{"first": a, "second": b} {
				select {
				case <-c.received:
				case err := <-c.errors:
					t.Fatalf("%s member: signaling failed: %v", name, err)
				case <-time.After(20 * time.Second):
					t.Fatalf("%s member received no audio through the relay", name)
				}
			}
			if st := relay.Status(); st.Allocations < 2 {
				t.Fatalf("allocations = %d, want both members relayed", st.Allocations)
			}
			if metrics.TURNRelayedBytes.Sum(0, "to_sfu") <= before {
				t.Fatal("no bytes counted through the relay")
			}
		})
	}
}
