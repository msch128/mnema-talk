package sfu

import (
	"context"
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

func gatherOffer(t *testing.T, s *SFU) string {
	t.Helper()
	pc, err := s.currentAPI().NewPeerConnection(webrtc.Configuration{})
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
