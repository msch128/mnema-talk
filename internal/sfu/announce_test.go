package sfu

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"net"
	"sync/atomic"

	"github.com/pion/webrtc/v4"
	"golang.org/x/net/dns/dnsmessage"
)

func TestResolveAnnounce(t *testing.T) {
	got, err := ResolveAnnounce(context.Background(), []string{"198.51.100.23", "192.168.1.20", "198.51.100.23", "localhost"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got[:2], []string{"198.51.100.23", "192.168.1.20"}) || !slices.Contains(got, "127.0.0.1") || len(got) != 3 {
		t.Fatalf("got %v", got)
	}
	if _, err := ResolveAnnounce(context.Background(), []string{"::1"}); err == nil {
		t.Fatal("IPv6 accepted")
	}
}

// Browsers must be offered every announced address, so members on the
// internet and in the LAN can both reach the server.
func TestOffersEveryAnnouncedAddress(t *testing.T) {
	s, err := NewSFU(0, 0, []string{"203.0.113.7", "192.168.1.20"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	offer := gatherOffer(t, s)
	for _, ip := range []string{"203.0.113.7", "192.168.1.20"} {
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
// local address, with one catch-all rule over all announced addresses.
func TestAnnounceRewriteRules(t *testing.T) {
	host := func(external []string) webrtc.ICEAddressRewriteRule {
		return webrtc.ICEAddressRewriteRule{
			External:        external,
			AsCandidateType: webrtc.ICECandidateTypeHost,
			Mode:            webrtc.ICEAddressRewriteReplace,
		}
	}
	cases := map[string]struct {
		ips  []string
		want []webrtc.ICEAddressRewriteRule
	}{
		"none":   {nil, []webrtc.ICEAddressRewriteRule{}},
		"single": {[]string{"203.0.113.7"}, []webrtc.ICEAddressRewriteRule{host([]string{"203.0.113.7"})}},
		"public and LAN": {
			[]string{"203.0.113.7", "192.168.1.20"},
			[]webrtc.ICEAddressRewriteRule{host([]string{"203.0.113.7", "192.168.1.20"})},
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
	ips := []string{"203.0.113.7", "192.168.1.20"}

	var legacy webrtc.SettingEngine
	legacy.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4})
	//lint:ignore SA1019 the deprecated call is the reference behaviour under test.
	legacy.SetNAT1To1IPs(ips, webrtc.ICECandidateTypeHost)
	var rewrite webrtc.SettingEngine
	rewrite.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4})
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

// offerCandidates lists the distinct candidates of the SDP as sorted "component
// protocol address typ type" strings, leaving out foundation, priority and the
// random port. The agent gathers one socket per local address and drops a
// candidate that repeats another's address and port. The OS may hand two
// sockets the same ephemeral port, so how often an address repeats is chance;
// which addresses are offered is not.
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
	return slices.Compact(out)
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

// Serve actual DNS responses on a local UDP socket. Resolver.Dial is the
// standard library's public transport seam; no external DNS is contacted.
func localAnnounceDNS(t *testing.T) *atomic.Int32 {
	t.Helper()
	conn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	state := &atomic.Int32{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 512)
		for {
			n, addr, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
			var query dnsmessage.Message
			if query.Unpack(buf[:n]) != nil {
				continue
			}
			response := dnsmessage.Message{
				Header:    dnsmessage.Header{ID: query.ID, Response: true, Authoritative: true, RecursionAvailable: true},
				Questions: query.Questions,
			}
			current := state.Load()
			if current < 0 {
				response.Header.RCode = dnsmessage.RCodeNameError
			} else {
				for _, q := range query.Questions {
					if q.Type == dnsmessage.TypeA {
						response.Answers = append(response.Answers, dnsmessage.Resource{
							Header: dnsmessage.ResourceHeader{Name: q.Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET},
							Body:   &dnsmessage.AResource{A: [4]byte{198, 51, 100, byte(current + 1)}},
						})
					}
				}
			}
			raw, err := response.Pack()
			if err == nil {
				_, _ = conn.WriteTo(raw, addr)
			}
		}
	}()
	old := net.DefaultResolver
	net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{Resolver: old}).DialContext(ctx, "udp4", conn.LocalAddr().String())
	}}
	t.Cleanup(func() {
		net.DefaultResolver = old
		_ = conn.Close()
		<-done
	})
	return state
}

func TestAnnounceRefreshTracksDNSAndPreservesAddressOnFailure(t *testing.T) {
	dns := localAnnounceDNS(t)
	logs := captureSFULog(t)
	s, err := NewSFU(0, 0, []string{"198.51.100.1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	// The loop reads the swapped resolver: it must be gone before the test restores it.
	t.Cleanup(func() { cancel(); s.announcers.Wait() })
	s.KeepAnnounceCurrent(ctx, []string{"announce.test"}, 5*time.Millisecond)
	// Repeated resolutions of an unchanged address must leave the API intact.
	api := s.currentAPI()
	time.Sleep(30 * time.Millisecond)
	if s.currentAPI() != api {
		t.Fatal("unchanged DNS response rebuilt the WebRTC API")
	}
	dns.Store(1)
	waitSFUCondition(t, "changed DNS announcement", func() bool {
		return slices.Equal(s.AnnouncedIPs(), []string{"198.51.100.2"})
	})
	if s.currentAPI() == api {
		t.Fatal("changed DNS address did not rebuild the API")
	}
	dns.Store(-1)
	waitSFUCondition(t, "DNS failure warning", func() bool {
		return strings.Contains(logs.String(), "resolve failed, keeping current addresses")
	})
	if !slices.Equal(s.AnnouncedIPs(), []string{"198.51.100.2"}) {
		t.Fatal("failed DNS resolution discarded the working address")
	}
	cancel()
	// Static announcements do not start a ticker, including when interval is zero.
	s.KeepAnnounceCurrent(context.Background(), []string{"198.51.100.2"}, 0)
}

func TestAnnounceRefreshReportsShutdownAndStopsOnCancellation(t *testing.T) {
	dns := localAnnounceDNS(t)
	logs := captureSFULog(t)
	s, err := NewSFU(0, 0, []string{"198.51.100.1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	dns.Store(1)
	ctx, cancel := context.WithCancel(context.Background())
	// The loop reads the swapped resolver: it must be gone before the test restores it.
	t.Cleanup(func() { cancel(); s.announcers.Wait() })
	s.KeepAnnounceCurrent(ctx, []string{"announce.test"}, 5*time.Millisecond)
	waitSFUCondition(t, "closed SFU announcement warning", func() bool {
		return strings.Contains(logs.String(), "webrtc announce: update failed")
	})
	cancel()
	if !slices.Equal(s.AnnouncedIPs(), []string{"198.51.100.1"}) {
		t.Fatal("refresh changed a shut down SFU")
	}
}
