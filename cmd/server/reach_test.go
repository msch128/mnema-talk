package main

import (
	"net/netip"
	"testing"

	"github.com/msch128/mnema-talk/internal/sfu"
)

func TestProvesOutsideReach(t *testing.T) {
	pub := netip.MustParseAddr("203.0.113.7")
	other := netip.MustParseAddr("198.51.100.9")
	announced := []string{"203.0.113.7", "192.168.1.5"}
	cases := []struct {
		name string
		ev   sfu.ICEConnected
		want bool
	}{
		{"srflx from outside", sfu.ICEConnected{Remote: other, RemoteType: "srflx"}, true},
		{"host from outside", sfu.ICEConnected{Remote: other, RemoteType: "host"}, true},
		{"relayed, even at a public address", sfu.ICEConnected{Remote: other, RemoteType: "relay"}, false},
		{"relay seen from the server at its own public address", sfu.ICEConnected{Remote: pub, RemoteType: "prflx"}, false},
		{"own announced address as srflx", sfu.ICEConnected{Remote: pub, RemoteType: "srflx"}, false},
	}
	for _, c := range cases {
		if got := provesOutsideReach(c.ev, announced); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}
