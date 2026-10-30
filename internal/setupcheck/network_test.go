package setupcheck

import (
	"bytes"
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func addr(s string) netip.Addr {
	if s == "" {
		return netip.Addr{}
	}
	return netip.MustParseAddr(s)
}

func TestIsCGNATCoversExactlyTheSharedAddressSpace(t *testing.T) {
	for in, want := range map[string]bool{
		"100.64.0.0": true, "100.100.100.100": true, "100.127.255.255": true,
		"100.63.255.255": false, "100.128.0.0": false, "10.0.0.1": false, "203.0.113.7": false,
		"::ffff:100.70.0.1": true,
	} {
		if got := IsCGNAT(netip.MustParseAddr(in)); got != want {
			t.Errorf("IsCGNAT(%s) = %v, want %v", in, got, want)
		}
	}
	if IsCGNAT(netip.Addr{}) {
		t.Error("an unset address is not CGNAT")
	}
}

func TestClassifyNetwork(t *testing.T) {
	for name, tc := range map[string]struct {
		f    NetworkFacts
		code string
	}{
		"healthy home connection":              {NetworkFacts{STUN: addr("203.0.113.7"), Local: addr("192.168.1.20")}, ""},
		"STUN answers with a CGNAT address":    {NetworkFacts{STUN: addr("100.72.1.9"), Local: addr("192.168.1.20")}, "network.cgnat"},
		"CGNAT host address, STUN failed":      {NetworkFacts{STUNFailed: true, Local: addr("100.64.3.2")}, "network.cgnat"},
		"CGNAT host address behind 1:1 NAT":    {NetworkFacts{STUN: addr("203.0.113.7"), Local: addr("100.64.3.2")}, ""},
		"IPv4 fails while IPv6 works":          {NetworkFacts{STUNFailed: true, IPv6OK: true, Local: addr("192.168.1.20")}, "network.no_ipv4"},
		"everything fails: no verdict":         {NetworkFacts{STUNFailed: true, Local: addr("192.168.1.20")}, ""},
		"public local address differs":         {NetworkFacts{STUN: addr("203.0.113.7"), Local: addr("198.51.100.4")}, "network.address_mismatch"},
		"public local address equals":          {NetworkFacts{STUN: addr("203.0.113.7"), Local: addr("203.0.113.7")}, ""},
		"private local address is just a NAT":  {NetworkFacts{STUN: addr("203.0.113.7"), Local: addr("10.0.0.5")}, ""},
		"nothing known":                        {NetworkFacts{}, ""},
		"CGNAT outranks a failed IPv4 request": {NetworkFacts{STUNFailed: true, IPv6OK: true, Local: addr("100.64.0.9")}, "network.cgnat"},
	} {
		t.Run(name, func(t *testing.T) {
			if code, _, _ := ClassifyNetwork(tc.f); code != tc.code {
				t.Fatalf("code = %q, want %q", code, tc.code)
			}
		})
	}
}

func TestEveryNetworkOutcomeHasAnActionableMessage(t *testing.T) {
	for _, code := range []string{codeCGNAT, codeNoIPv4, codeAddressMismatch} {
		c := networkCheck(code, Warn, Params{"address": "100.64.0.1", "local": "198.51.100.4", "public": "203.0.113.7"})
		if c.Level == OK || c.Fix == "" || strings.Contains(c.Detail+c.Fix, "{") {
			t.Errorf("%s: %+v", code, c)
		}
	}
	c := networkCheck(codeCGNAT, Fail, Params{"address": "100.64.0.1"})
	for _, want := range []string{"public IPv4", "VPS", "external relay", "Can I host at home?"} {
		if !strings.Contains(c.Fix, want) {
			t.Errorf("CGNAT fix lacks %q: %s", want, c.Fix)
		}
	}
	if c.Level != Fail {
		t.Errorf("CGNAT level = %s", c.Level)
	}
}

func TestJudgeNetworkShowsUpInTheChecks(t *testing.T) {
	cfg := prodConfig(t, nil)
	rt := &Runtime{}
	rt.SetAnnounce(SourceDetected, []string{"100.72.1.9"}, "")
	ann := Announcement{DetectedIP: addr("100.72.1.9"), Source: SourceDetected}
	JudgeNetwork(context.Background(), cfg, ann, Host{}, nil, rt)
	in := healthy(cfg)
	in.Runtime = rt
	got := byID(Run(in), Network)
	if got.Code != "network.cgnat" || got.Level != Fail || got.Params["address"] != "100.72.1.9" {
		t.Fatalf("network check = %+v", got)
	}
	if byID(Run(healthy(cfg)), Network).ID != "" {
		t.Fatal("a healthy connection adds no network check")
	}
}

func TestHostCGNATWithoutASTUNAnswerIsOnlyAmber(t *testing.T) {
	cfg := prodConfig(t, nil)
	rt := &Runtime{}
	JudgeNetwork(context.Background(), cfg, Announcement{DetectErr: "no answer"}, Host{Local: addr("100.64.3.2")}, nil, rt)
	in := healthy(cfg)
	in.Runtime = rt
	if got := byID(Run(in), Network); got.Code != "network.cgnat" || got.Level != Warn {
		t.Fatalf("network check = %+v", got)
	}
}

func TestAnAnnouncedAddressTheOperatorChoseIsNotJudged(t *testing.T) {
	rt := &Runtime{}
	cfg := prodConfig(t, map[string]string{"WEBRTC_NAT_1TO1_IP": "203.0.113.7"})
	JudgeNetwork(context.Background(), cfg, Announcement{DetectErr: "x"}, Host{Local: addr("100.64.3.2")}, nil, rt)
	if rt.snapshot().networkCode != "" {
		t.Fatal("judged despite WEBRTC_NAT_1TO1_IP")
	}
}

func TestJudgeNetworkAsksIPv6OnlyAfterIPv4Failed(t *testing.T) {
	cfg := prodConfig(t, nil)
	calls := 0
	v6 := func(context.Context, []string, time.Duration) (netip.Addr, string, error) {
		calls++
		return addr("2001:db8::7"), "stun.test", nil
	}
	rt := &Runtime{}
	JudgeNetwork(context.Background(), cfg, Announcement{DetectedIP: addr("203.0.113.7")}, Host{}, v6, rt)
	if calls != 0 {
		t.Fatal("IPv6 STUN asked although IPv4 worked")
	}
	JudgeNetwork(context.Background(), cfg, Announcement{DetectErr: "no answer"}, Host{}, v6, rt)
	if calls != 1 || rt.snapshot().networkCode != "network.no_ipv4" {
		t.Fatalf("calls %d, code %q", calls, rt.snapshot().networkCode)
	}
}

func TestJudgeNetworkLeavesDevelopmentAndTrialsAlone(t *testing.T) {
	for _, env := range []map[string]string{{"APP_ENV": "development", "PUBLIC_URL": "http://localhost:8080", "BIND_ADDR": "127.0.0.1"}, {"PUBLIC_URL": "http://localhost:8080"}} {
		rt := &Runtime{}
		JudgeNetwork(context.Background(), prodConfig(t, env), Announcement{DetectedIP: addr("100.72.1.9")}, Host{}, nil, rt)
		if rt.snapshot().networkCode != "" {
			t.Errorf("%v: judged", env)
		}
	}
}

func TestLANAnnounceSelection(t *testing.T) {
	lan := Host{Local: addr("192.168.1.20")}
	for name, tc := range map[string]struct {
		env  map[string]string
		host Host
		want string
	}{
		"explicit WEBRTC_LAN_IP wins, even in a container":  {map[string]string{"WEBRTC_LAN_IP": "192.168.1.50, 10.0.0.2"}, Host{InContainer: true, Local: addr("172.18.0.5")}, "192.168.1.50,10.0.0.2"},
		"host outside a container uses its private address": {nil, lan, "192.168.1.20"},
		"a container never guesses":                         {nil, Host{InContainer: true, Local: addr("192.168.1.20")}, ""},
		"a public local address is not a LAN address":       {nil, Host{Local: addr("203.0.113.7")}, ""},
		"CGNAT is not a LAN address":                        {nil, Host{Local: addr("100.64.3.2")}, ""},
		"loopback is not a LAN address":                     {nil, Host{Local: addr("127.0.0.1")}, ""},
		"unknown local address":                             {nil, Host{}, ""},
	} {
		t.Run(name, func(t *testing.T) {
			got := strings.Join(LANAnnounce(prodConfig(t, tc.env), tc.host), ",")
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestResolveAnnouncementAddsTheLANAddressAfterThePublicOne(t *testing.T) {
	ok := detector("203.0.113.7", nil)
	fail := detector("", errors.New("no answer"))
	lan := Host{Local: addr("192.168.1.20")}
	for name, tc := range map[string]struct {
		env     map[string]string
		detect  Detector
		host    Host
		entries string
		lan     string
	}{
		"detected public address then LAN":           {nil, ok, lan, "203.0.113.7,192.168.1.20", "192.168.1.20"},
		"WEBRTC_LAN_IP in a container":               {map[string]string{"WEBRTC_LAN_IP": "192.168.1.50"}, ok, Host{InContainer: true}, "203.0.113.7,192.168.1.50", "192.168.1.50"},
		"container without WEBRTC_LAN_IP":            {nil, ok, Host{InContainer: true, Local: addr("172.18.0.5")}, "203.0.113.7", ""},
		"explicit WEBRTC_NAT_1TO1_IP is left alone":  {map[string]string{"WEBRTC_NAT_1TO1_IP": "nas.example.org", "WEBRTC_LAN_IP": "192.168.1.50"}, ok, lan, "nas.example.org", ""},
		"no public address, nothing to append to":    {nil, fail, lan, "", ""},
		"LAN address already in PUBLIC_URL is a dup": {map[string]string{"PUBLIC_URL": "http://192.168.1.20:8080"}, ok, lan, "192.168.1.20,203.0.113.7", ""},
		"PUBLIC_URL on this machine: a trial":        {map[string]string{"PUBLIC_URL": "http://localhost:8080"}, fail, lan, "127.0.0.1,192.168.1.20", "192.168.1.20"},
	} {
		t.Run(name, func(t *testing.T) {
			got := ResolveAnnouncement(context.Background(), prodConfig(t, tc.env), tc.detect, tc.host)
			if strings.Join(got.Entries, ",") != tc.entries || strings.Join(got.LAN, ",") != tc.lan {
				t.Fatalf("entries %v lan %v, want %q / %q", got.Entries, got.LAN, tc.entries, tc.lan)
			}
		})
	}
}

func TestResolveAnnouncementKeepsTheDetectedAddress(t *testing.T) {
	got := ResolveAnnouncement(context.Background(), prodConfig(t, nil), detector("203.0.113.7", nil), Host{})
	if got.DetectedIP != addr("203.0.113.7") {
		t.Fatalf("DetectedIP = %v", got.DetectedIP)
	}
}

func TestReportListsFixesUnderTheProblemsAndReturnsTheWorstLevel(t *testing.T) {
	checks := []Check{
		{ID: "a", Level: OK, Detail: "All fine.", Fix: "ignored when ok"},
		{ID: "b", Level: Warn, Detail: "Almost.", Fix: "Do this."},
		{ID: "c", Level: Fail, Detail: "Broken.", Fix: "Fix that."},
	}
	var buf bytes.Buffer
	if worst := Report(&buf, checks); worst != Fail {
		t.Fatalf("worst = %s", worst)
	}
	want := "[ ok ] All fine.\n[warn] Almost.\n       fix: Do this.\n[FAIL] Broken.\n       fix: Fix that.\n\n1 of 3 checks ok, 2 need attention.\n"
	if buf.String() != want {
		t.Fatalf("got:\n%s\nwant:\n%s", buf.String(), want)
	}
}
