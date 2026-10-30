//go:build integration

package server

import (
	"net/http"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/msch128/mnema-talk/internal/setupcheck"
	"github.com/msch128/mnema-talk/internal/sfu"
	"github.com/pion/webrtc/v4"
)

// browserOffer builds what the browser sends: one data channel, all
// candidates gathered.
func browserOffer(t *testing.T) string {
	t.Helper()
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	if _, err := pc.CreateDataChannel("probe", nil); err != nil {
		t.Fatal(err)
	}
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	done := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("gathering timed out")
	}
	return pc.LocalDescription().SDP
}

func appWithVoice(t *testing.T, rt *setupcheck.Runtime, withHub bool) *app {
	t.Helper()
	voice, err := sfu.NewSFU(0, 0, []string{"127.0.0.1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = voice.Close() })
	return newAppWithDeps(t, withHub, nil, func(d *Deps) { d.SFU, d.Setup = voice, rt })
}

func TestConnectionProbeNeedsASessionAndAnOffer(t *testing.T) {
	a := appWithVoice(t, &setupcheck.Runtime{}, false)
	admin := a.seedAdmin()
	member := a.register(admin, "pat")

	if res := a.anon().post("/api/webrtc/probe", map[string]string{"sdp": "x"}); res.status != http.StatusUnauthorized {
		t.Errorf("anonymous: %d", res.status)
	}
	if res := member.post("/api/webrtc/probe", map[string]string{"sdp": "v=0"}); res.status != http.StatusBadRequest {
		t.Errorf("garbage offer: %d %s", res.status, res.body)
	}

	res := member.post("/api/webrtc/probe", map[string]string{"sdp": browserOffer(t)})
	if res.status != http.StatusOK || res.header.Get("Cache-Control") != "no-store" {
		t.Fatalf("probe: %d %s", res.status, res.body)
	}
	var out ConnectionProbeAnswer
	res.decode(t, &out)
	if !strings.Contains(out.SDP, "a=candidate:") || strings.Contains(out.SDP, "m=audio") {
		t.Errorf("answer = %q", out.SDP)
	}
}

func TestConnectionProbeIsRateLimitedPerMember(t *testing.T) {
	a := appWithVoice(t, &setupcheck.Runtime{}, false)
	member := a.register(a.seedAdmin(), "pat")
	var limited bool
	for i := 0; i < 20 && !limited; i++ {
		res := member.post("/api/webrtc/probe", map[string]string{"sdp": "v=0"})
		limited = res.status == http.StatusTooManyRequests
		if limited && res.header.Get("Retry-After") == "" {
			t.Error("429 without Retry-After")
		}
	}
	if !limited {
		t.Fatal("twenty tests in a row were all accepted")
	}
	// Another member has a budget of their own.
	other := a.register(a.login("cool_guy", "admin-password-123"), "kim")
	if res := other.post("/api/webrtc/probe", map[string]string{"sdp": "v=0"}); res.status != http.StatusBadRequest {
		t.Errorf("second member: %d", res.status)
	}
}

func TestConnectionProbeWithoutVoiceIsUnavailable(t *testing.T) {
	a := newAppWithDeps(t, false, nil, nil)
	member := a.register(a.seedAdmin(), "pat")
	if res := member.post("/api/webrtc/probe", map[string]string{"sdp": "v=0"}); res.status != http.StatusServiceUnavailable {
		t.Errorf("no SFU: %d %s", res.status, res.body)
	}
}

func TestWebSocketReportIsAdminOnlyAndShowsInTheStatus(t *testing.T) {
	rt := &setupcheck.Runtime{}
	a := appWithVoice(t, rt, false)
	admin := a.seedAdmin()
	member := a.register(admin, "pat")

	if res := member.post("/api/admin/status/websocket", map[string]bool{"connected": true}); res.status != http.StatusForbidden {
		t.Errorf("member: %d", res.status)
	}
	if res := admin.post("/api/admin/status/websocket", "nope"); res.status != http.StatusBadRequest {
		t.Errorf("malformed: %d", res.status)
	}
	level := func() setupcheck.Check {
		var st OperatorStatus
		admin.get("/api/admin/status").decode(t, &st)
		return setupCheckByID(t, st, "websocket")
	}
	if c := level(); c.Level != setupcheck.Info {
		t.Fatalf("before any report: %+v", c)
	}
	if res := admin.post("/api/admin/status/websocket", map[string]bool{"connected": true}); res.status != http.StatusNoContent {
		t.Fatalf("report: %d %s", res.status, res.body)
	}
	if c := level(); c.Level != setupcheck.OK || !strings.Contains(c.Detail, "WebSocket via proxy OK") {
		t.Fatalf("after ok: %+v", c)
	}
	// A socket that is merely not open yet proves nothing about the proxy:
	// reports of false change nothing, however many.
	for i := 0; i < 5; i++ {
		if res := admin.post("/api/admin/status/websocket", map[string]bool{"connected": false}); res.status != http.StatusNoContent {
			t.Fatalf("false report: %d", res.status)
		}
	}
	if c := level(); c.Level != setupcheck.OK {
		t.Fatalf("after five false reports: %+v", c)
	}
}

func TestStatusSeesRealWebSocketUpgradesAndMisses(t *testing.T) {
	rt := &setupcheck.Runtime{}
	a := appWithVoice(t, rt, true)
	admin := a.seedAdmin()
	wsCheck := func() setupcheck.Check {
		var st OperatorStatus
		admin.get("/api/admin/status").decode(t, &st)
		return setupCheckByID(t, st, "websocket")
	}

	// A proxy that strips the upgrade headers: the request arrives as a
	// plain GET.
	for i := 0; i < 3; i++ {
		admin.get("/api/ws")
	}
	if c := wsCheck(); c.Level != setupcheck.Warn {
		t.Fatalf("three plain requests: %+v", c)
	}

	if _, _, err := admin.dialWS(a.origin()); err != nil {
		t.Fatal(err)
	}
	if c := wsCheck(); c.Level != setupcheck.OK {
		t.Fatalf("after a real upgrade: %+v", c)
	}
}

func TestUnauthenticatedWebSocketRequestsDoNotFlagTheProxy(t *testing.T) {
	rt := &setupcheck.Runtime{}
	a := appWithVoice(t, rt, true)
	admin := a.seedAdmin()
	for i := 0; i < 5; i++ {
		a.anon().get("/api/ws")
	}
	var st OperatorStatus
	admin.get("/api/admin/status").decode(t, &st)
	if c := setupCheckByID(t, st, "websocket"); c.Level != setupcheck.Info {
		t.Fatalf("anonymous probing flagged the proxy: %+v", c)
	}
}

func TestVoiceUDPTurnsGreenOnlyForAnOutsidePeer(t *testing.T) {
	rt := &setupcheck.Runtime{}
	a := appWithVoice(t, rt, false)
	admin := a.seedAdmin()
	voice := func() setupcheck.Check {
		var st OperatorStatus
		admin.get("/api/admin/status").decode(t, &st)
		return setupCheckByID(t, st, "voice_udp")
	}
	if c := voice(); c.Level != setupcheck.Info || c.Code != "voice_udp.unconfirmed" {
		t.Fatalf("fresh start: %+v", c)
	}
	rt.RecordICEConnected(netip.MustParseAddr("192.168.1.4"), time.Now())
	if c := voice(); c.Level != setupcheck.Info {
		t.Fatalf("LAN peer: %+v", c)
	}
	rt.RecordICEConnected(netip.MustParseAddr("198.51.100.20"), time.Now())
	if c := voice(); c.Level != setupcheck.OK || !strings.HasPrefix(c.Detail, "Confirmed: a member from outside connected") {
		t.Fatalf("outside peer: %+v", c)
	}
}

func TestConnectionProbeLimitsLiveTestsPerMember(t *testing.T) {
	a := appWithVoice(t, &setupcheck.Runtime{}, false)
	member := a.register(a.seedAdmin(), "pat")
	offer := browserOffer(t)
	for i := 0; i < 2; i++ {
		if res := member.post("/api/webrtc/probe", map[string]string{"sdp": offer}); res.status != http.StatusOK {
			t.Fatalf("probe %d: %d %s", i, res.status, res.body)
		}
	}
	if res := member.post("/api/webrtc/probe", map[string]string{"sdp": offer}); res.status != http.StatusTooManyRequests {
		t.Fatalf("third live probe: %d", res.status)
	}
}
