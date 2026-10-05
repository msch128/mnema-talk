package sfu

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
)

func TestResolveAnnounce(t *testing.T) {
	got, err := ResolveAnnounce(context.Background(), []string{"91.66.75.113", "192.168.0.212", "91.66.75.113", "localhost"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got[:2], []string{"91.66.75.113", "192.168.0.212"}) || !slices.Contains(got, "127.0.0.1") || len(got) != 3 {
		t.Fatalf("got %v", got)
	}
	if _, err := ResolveAnnounce(context.Background(), []string{"::1"}); err == nil {
		t.Fatal("IPv6 accepted")
	}
}

// Browsers must be offered every announced address, so members on the
// internet and in the LAN can both reach the server.
func TestOffersEveryAnnouncedAddress(t *testing.T) {
	s, err := NewSFU(0, 0, []string{"203.0.113.7", "192.168.0.212"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	offer := gatherOffer(t, s)
	for _, ip := range []string{"203.0.113.7", "192.168.0.212"} {
		if !strings.Contains(offer, " "+ip+" ") {
			t.Errorf("offer has no candidate for %s:\n%s", ip, offer)
		}
	}

	// Switching addresses (new public IP) applies to the next connection.
	if err := s.SetAnnouncedIPs([]string{"198.51.100.9"}); err != nil {
		t.Fatal(err)
	}
	if offer := gatherOffer(t, s); !strings.Contains(offer, " 198.51.100.9 ") || strings.Contains(offer, " 203.0.113.7 ") {
		t.Errorf("after update the offer still announces old addresses:\n%s", offer)
	}
}

// The rules must be what Pion built from the deprecated
// SetNAT1To1IPs(ips, ICECandidateTypeHost): host candidates that replace the
// local address, one catch-all rule over all external addresses, plus a
// pinned rule per "external/local" entry.
func TestAnnounceRewriteRules(t *testing.T) {
	host := func(external []string, local string) webrtc.ICEAddressRewriteRule {
		return webrtc.ICEAddressRewriteRule{
			External:        external,
			Local:           local,
			AsCandidateType: webrtc.ICECandidateTypeHost,
			Mode:            webrtc.ICEAddressRewriteReplace,
		}
	}
	cases := map[string]struct {
		ips  []string
		want []webrtc.ICEAddressRewriteRule
	}{
		"none":   {nil, []webrtc.ICEAddressRewriteRule{}},
		"single": {[]string{"203.0.113.7"}, []webrtc.ICEAddressRewriteRule{host([]string{"203.0.113.7"}, "")}},
		"public and LAN": {
			[]string{"203.0.113.7", "192.168.0.212"},
			[]webrtc.ICEAddressRewriteRule{host([]string{"203.0.113.7", "192.168.0.212"}, "")},
		},
		"pinned to a local address": {
			[]string{"203.0.113.7/10.0.0.2", "192.168.0.212"},
			[]webrtc.ICEAddressRewriteRule{
				host([]string{"203.0.113.7"}, "10.0.0.2"),
				host([]string{"203.0.113.7", "192.168.0.212"}, ""),
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := announceRewriteRules(tc.ips)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got  %+v\nwant %+v", got, tc.want)
			}
		})
	}
}

// The rewrite rules must offer exactly the candidates the deprecated
// SetNAT1To1IPs(ips, ICECandidateTypeHost) offered: the announced addresses
// as host candidates and none of the container's own addresses.
func TestAnnounceRulesMatchLegacyNAT1To1(t *testing.T) {
	ips := []string{"203.0.113.7", "192.168.0.212"}

	var legacy webrtc.SettingEngine
	//lint:ignore SA1019 the deprecated call is the reference behaviour under test.
	legacy.SetNAT1To1IPs(ips, webrtc.ICECandidateTypeHost)
	var rewrite webrtc.SettingEngine
	if err := rewrite.SetICEAddressRewriteRules(announceRewriteRules(ips)...); err != nil {
		t.Fatal(err)
	}

	want := offerCandidates(t, gatherSDP(t, webrtc.NewAPI(webrtc.WithSettingEngine(legacy))))
	got := offerCandidates(t, gatherSDP(t, webrtc.NewAPI(webrtc.WithSettingEngine(rewrite))))
	if !slices.Equal(got, want) {
		t.Fatalf("candidates differ from SetNAT1To1IPs:\ngot  %v\nwant %v", got, want)
	}
	if len(got) == 0 {
		t.Fatal("no candidates gathered")
	}
	for _, c := range got {
		if !strings.HasSuffix(c, " typ host") || !slices.ContainsFunc(ips, func(ip string) bool { return strings.Contains(c, " "+ip+" ") }) {
			t.Errorf("candidate is not an announced host address: %s", c)
		}
	}
}

// offerCandidates lists the SDP's candidates as sorted "protocol address typ
// type" strings, leaving out foundation, priority and the random port.
func offerCandidates(t *testing.T, sdp string) []string {
	t.Helper()
	var out []string
	for line := range strings.Lines(sdp) {
		f := strings.Fields(strings.TrimPrefix(strings.TrimSpace(line), "a=candidate:"))
		if !strings.HasPrefix(line, "a=candidate:") || len(f) < 8 {
			continue
		}
		// foundation component protocol priority address port "typ" type ...
		out = append(out, strings.Join([]string{f[1], strings.ToLower(f[2]), f[4], f[6], f[7]}, " "))
	}
	slices.Sort(out)
	return out
}

func gatherOffer(t *testing.T, s *SFU) string {
	t.Helper()
	return gatherSDP(t, s.currentAPI())
}

func gatherSDP(t *testing.T, api *webrtc.API) string {
	t.Helper()
	pc, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	if _, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio); err != nil {
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
		t.Fatal("ICE gathering timed out")
	}
	return pc.LocalDescription().SDP
}
