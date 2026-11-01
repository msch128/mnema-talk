//go:build integration

package server

import (
	"net/http"
	"testing"
)

func TestSpeedTestNeedsASessionAndStreamsToAMember(t *testing.T) {
	a := newApp(t, false)
	member := a.register(a.seedAdmin(), "pat")
	if res := a.anon().get("/api/webrtc/speedtest"); res.status != http.StatusUnauthorized {
		t.Errorf("anonymous: %d", res.status)
	}
	res := member.get("/api/webrtc/speedtest")
	if res.status != http.StatusOK || len(res.body) == 0 || len(res.body) > speedMaxBytes || res.header.Get("Cache-Control") != "no-store" {
		t.Fatalf("member: %d, %d bytes", res.status, len(res.body))
	}
}
