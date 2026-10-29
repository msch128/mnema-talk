package main

import (
	"net"
	"strconv"
	"testing"

	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/opwarn"
	"github.com/msch128/mnema-talk/internal/sfu"
	"github.com/msch128/mnema-talk/internal/turnrelay"
)

func freeUDP(t *testing.T) uint16 {
	t.Helper()
	c, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	return uint16(c.LocalAddr().(*net.UDPAddr).Port)
}

func testVoice(t *testing.T) *sfu.SFU {
	t.Helper()
	port := freeUDP(t)
	voice, err := sfu.NewSFU(port, port, []string{"127.0.0.1"}, nil, sfu.WithUDPMuxPort(port))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = voice.Close() })
	return voice
}

func hasWarning(code string) bool {
	for _, w := range opwarn.Default.List() {
		if w.Code == code {
			return true
		}
	}
	return false
}

func TestStartTURNHonoursTheSwitch(t *testing.T) {
	voice := testVoice(t)
	external := []string{"turn:turn.example.test:3478?transport=udp"}
	for name, cfg := range map[string]*config.Config{
		"off":                   {WebRTCBuiltinTURN: config.BuiltinTURNOff},
		"auto with external":    {WebRTCBuiltinTURN: config.BuiltinTURNAuto, WebRTCTURNURLs: external},
		"default with external": {WebRTCTURNURLs: external},
		"nothing without voice": {WebRTCBuiltinTURN: config.BuiltinTURNOn},
	} {
		v := voice
		if name == "nothing without voice" {
			v = nil
		}
		relay, closeRelay := startTURN(cfg, v)
		if relay != nil {
			t.Errorf("%s: relay started", name)
		}
		closeRelay()
	}
}

func TestStartTURNRunsInAutoWithoutExternalTURN(t *testing.T) {
	voice := testVoice(t)
	var relay *turnrelay.Server
	// A free port can be taken between picking it and binding it: try again
	// a few times, but a relay that never starts is a failure.
	for range 5 {
		probe, err := net.ListenPacket("udp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := probe.LocalAddr().(*net.UDPAddr).Port
		_ = probe.Close()
		cfg := &config.Config{WebRTCBuiltinTURN: config.BuiltinTURNAuto, WebRTCBuiltinTURNPort: uint16(port), PublicURL: "http://127.0.0.1:8080"}
		var closeRelay func()
		relay, closeRelay = startTURN(cfg, voice)
		defer closeRelay()
		if relay != nil {
			break
		}
	}
	if relay == nil {
		t.Fatal("the relay did not start in auto mode without an external TURN server")
	}
	if st := relay.Status(); !st.UDP || !st.TCP {
		t.Fatalf("status = %+v", st)
	}
}

// A port that cannot be bound never stops the server: no relay, a warning for
// the admin, voice untouched.
func TestStartTURNKeepsRunningWhenThePortIsTaken(t *testing.T) {
	udp, err := net.ListenPacket("udp4", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	defer udp.Close()
	port := udp.LocalAddr().(*net.UDPAddr).Port
	tcp, err := net.Listen("tcp4", "0.0.0.0:"+strconv.Itoa(port))
	if err != nil {
		t.Skip("the TCP side of the port is taken as well")
	}
	defer tcp.Close()

	opwarn.Default.Clear(opwarn.TURNUnavailable)
	t.Cleanup(func() { opwarn.Default.Clear(opwarn.TURNUnavailable) })
	cfg := &config.Config{WebRTCBuiltinTURN: config.BuiltinTURNOn, WebRTCBuiltinTURNPort: uint16(port), PublicURL: "http://127.0.0.1:8080"}
	relay, closeRelay := startTURN(cfg, testVoice(t))
	closeRelay()
	if relay != nil {
		t.Fatal("a relay started on a taken port")
	}
	if !hasWarning(opwarn.TURNUnavailable) {
		t.Fatal("no operator warning for the relay that could not start")
	}
}
