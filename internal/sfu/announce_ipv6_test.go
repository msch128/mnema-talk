package sfu

import (
	"net"
	"strings"
	"testing"
)

func TestAnnounceIPFilter(t *testing.T) {
	if announceIPFilter(nil) != nil {
		t.Fatal("a filter without announced addresses")
	}
	cases := []struct {
		announce []string
		ip       string
		want     bool
	}{
		{[]string{"203.0.113.7"}, "172.18.0.2", true},
		{[]string{"203.0.113.7"}, "2001:db8::5", true},
		{[]string{"203.0.113.7"}, "fd00::5", false},
		{[]string{"203.0.113.7"}, "fe80::1", false},
		{[]string{"203.0.113.7"}, "::1", false},
		{[]string{"127.0.0.1", "203.0.113.7"}, "::1", true},
		{[]string{"127.0.0.1"}, "127.0.0.1", true},
		{[]string{"127.0.0.1"}, "2001:db8::5", false},
		{[]string{"127.0.0.1"}, "172.18.0.2", false},
	}
	for _, c := range cases {
		if got := announceIPFilter(c.announce)(net.ParseIP(c.ip)); got != c.want {
			t.Errorf("announce %v, address %s: kept %v, want %v", c.announce, c.ip, got, c.want)
		}
	}
}

// Whatever interfaces the machine has, an offer built for announced IPv4
// addresses carries the announced address and no IPv6 address a browser could
// not reach (unique-local, link-local, loopback). The filter test above
// covers each class; this one checks the filter is wired into gathering.
func TestOfferCarriesNoUnroutableIPv6(t *testing.T) {
	s, err := NewSFU(0, 0, []string{"203.0.113.7"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	offer := gatherOffer(t, s)
	if !strings.Contains(offer, " 203.0.113.7 ") {
		t.Fatalf("offer lacks the announced address:\n%s", offer)
	}
	for _, line := range strings.Split(offer, "\n") {
		if !strings.HasPrefix(line, "a=candidate:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		ip := net.ParseIP(fields[4])
		if ip != nil && ip.To4() == nil && (ip.IsPrivate() || !ip.IsGlobalUnicast()) {
			t.Errorf("offer carries an unreachable IPv6 candidate: %s", line)
		}
	}
}
