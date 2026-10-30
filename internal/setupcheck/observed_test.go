package setupcheck

import (
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestOutsideAddresses(t *testing.T) {
	for addr, want := range map[string]bool{
		"203.0.113.7":    true,
		"8.8.8.8":        true,
		"2a00:1450::1":   true,
		"::ffff:1.2.3.4": true,
		"10.0.0.5":       false,
		"192.168.1.20":   false,
		"172.17.0.2":     false,
		"127.0.0.1":      false,
		"169.254.3.3":    false,
		"100.64.1.1":     false, // carrier-grade NAT is not "outside" of the provider's network
		"fd00::1":        false,
		"fe80::1":        false,
		"0.0.0.0":        false,
	} {
		if got := IsOutsideAddress(netip.MustParseAddr(addr)); got != want {
			t.Errorf("IsOutsideAddress(%s) = %v, want %v", addr, got, want)
		}
	}
	if IsOutsideAddress(netip.Addr{}) {
		t.Error("an invalid address is not outside")
	}
}

func TestVoiceUDPIsNeutralUntilSomeoneFromOutsideConnected(t *testing.T) {
	in := healthy(prodConfig(t, nil))
	c := byID(Run(in), VoiceUDP)
	if c.Level != Info || c.Code != "voice_udp.unconfirmed" || !strings.HasPrefix(c.Detail, "Listening – not yet confirmed from outside") || c.Fix == "" {
		t.Fatalf("before any peer: %+v", c)
	}

	now := time.Date(2026, 10, 29, 21, 30, 0, 0, time.UTC)
	in.Runtime.RecordICEConnected(netip.MustParseAddr("192.168.1.9"), now)
	in.Runtime.RecordICEConnected(netip.MustParseAddr("10.1.2.3"), now)
	in.Runtime.RecordICEConnected(netip.MustParseAddr("127.0.0.1"), now)
	if c := byID(Run(in), VoiceUDP); c.Level != Info {
		t.Fatalf("a LAN peer confirmed the port: %+v", c)
	}

	in.Runtime.RecordICEConnected(netip.MustParseAddr("203.0.113.50"), now)
	c = byID(Run(in), VoiceUDP)
	if c.Level != OK || c.Code != "voice_udp.confirmed" || c.Fix != "" ||
		!strings.HasPrefix(c.Detail, "Confirmed: a member from outside connected (2026-10-29 21:30 UTC)") ||
		c.Params["at"] != "2026-10-29T21:30:00Z" {
		t.Fatalf("after an outside peer: %+v", c)
	}
}

func TestVoiceUDPFailuresStillWinOverTheNeutralState(t *testing.T) {
	// The engine not running is a red item whatever was seen before.
	in := healthy(prodConfig(t, nil))
	in.Runtime.RecordICEConnected(netip.MustParseAddr("203.0.113.50"), time.Now())
	has := map[string]bool{"voice_unavailable": true}
	if c := voiceUDPCheck(in.Cfg, has, in.Runtime.snapshot()); c.Level != Fail {
		t.Fatalf("voice_udp = %+v", c)
	}
}

func TestWebSocketCheck(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	defer func(f func() time.Time) { nowFunc = f }(nowFunc)
	nowFunc = func() time.Time { return now }
	in := healthy(prodConfig(t, nil))
	ws := func() Check { return byID(Run(in), WebSocket) }

	if c := ws(); c.Level != Info || c.Code != "websocket.unchecked" {
		t.Fatalf("nothing seen: %+v", c)
	}

	in.Runtime.RecordWebSocket(false, now.Add(-time.Minute))
	in.Runtime.RecordWebSocket(false, now.Add(-time.Minute))
	if c := ws(); c.Level != Info {
		t.Fatalf("two misses are not yet repeated failure: %+v", c)
	}
	in.Runtime.RecordWebSocket(false, now.Add(-30*time.Second))
	c := ws()
	if c.Level != Warn || c.Code != "websocket.failing" || c.Params["count"] != "3" ||
		!strings.Contains(c.Fix, "Upgrade: websocket") || !strings.Contains(c.Fix, "Connection: Upgrade") || !strings.Contains(c.Fix, "Websockets Support") {
		t.Fatalf("three misses: %+v", c)
	}

	in.Runtime.RecordWebSocket(true, now.Add(-10*time.Second))
	c = ws()
	if c.Level != OK || c.Code != "websocket.ok" || !strings.Contains(c.Detail, "2026-10-29 11:59 UTC") || c.Fix != "" {
		t.Fatalf("after a success: %+v", c)
	}

	// Misses older than the window are forgotten.
	in.Runtime.RecordWebSocket(false, now.Add(-time.Hour))
	in.Runtime.RecordWebSocket(false, now.Add(-time.Hour))
	in.Runtime.RecordWebSocket(false, now.Add(-time.Hour))
	if c := ws(); c.Level != OK {
		t.Fatalf("stale misses flagged: %+v", c)
	}
}

func TestWebSocketMissesAreBounded(t *testing.T) {
	rt := &Runtime{}
	for i := 0; i < 1000; i++ {
		rt.RecordWebSocket(false, time.Now())
	}
	if n := len(rt.snapshot().wsFails); n != wsFailKept {
		t.Fatalf("kept %d misses, want %d", n, wsFailKept)
	}
}

func TestNeutralChecksAreNotProblems(t *testing.T) {
	checks := Run(healthy(prodConfig(t, nil)))
	neutral := []Check{byID(checks, VoiceUDP), byID(checks, WebSocket)}
	if neutral[0].Level != Info || neutral[1].Level != Info {
		t.Fatalf("want two neutral checks, got %+v", neutral)
	}
	if w := Worst(neutral); w != OK {
		t.Fatalf("worst = %s with only neutral items", w)
	}
}
