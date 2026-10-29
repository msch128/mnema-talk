package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/msch128/mnema-talk/loadtest/viewer/internal/lt"
	"github.com/pion/webrtc/v4"
)

// rtcConfiguration is the member's peer connection configuration. Normally it
// is empty: the SFU's own candidates are reachable directly. In relay mode
// (RELAY=1) the member takes the ICE servers the web client would get from
// /api/webrtc/config and may use relayed candidates only, so every packet goes
// through the server's built-in TURN relay.
func (c viewerConfig) rtcConfiguration(tk token) (webrtc.Configuration, error) {
	if !c.relay {
		return webrtc.Configuration{}, nil
	}
	servers, err := fetchTURNServers(c.base, tk, c.relayHost)
	if err != nil {
		return webrtc.Configuration{}, err
	}
	return webrtc.Configuration{ICEServers: servers, ICETransportPolicy: webrtc.ICETransportPolicyRelay}, nil
}

type iceConfigWire struct {
	ICEServers []struct {
		URLs       []string `json:"urls"`
		Username   string   `json:"username"`
		Credential string   `json:"credential"`
	} `json:"ice_servers"`
}

// fetchTURNServers asks the server for the member's ICE servers and keeps the
// TURN ones. hostOverride replaces the host of their URLs (the port stays),
// for a host that cannot reach its own public address.
func fetchTURNServers(base *url.URL, tk token, hostOverride string) ([]webrtc.ICEServer, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base.String()+"/api/webrtc/config", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Cookie", lt.CookieName(base)+"="+tk.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("webrtc config: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("webrtc config: %s", resp.Status)
	}
	var wire iceConfigWire
	if err := json.NewDecoder(resp.Body).Decode(&wire); err != nil {
		return nil, fmt.Errorf("webrtc config: %w", err)
	}
	return turnServers(wire, hostOverride)
}

// turnServers keeps the TURN servers of an ICE configuration.
func turnServers(wire iceConfigWire, hostOverride string) ([]webrtc.ICEServer, error) {
	var out []webrtc.ICEServer
	for _, s := range wire.ICEServers {
		var urls []string
		for _, u := range s.URLs {
			if strings.HasPrefix(u, "turn:") || strings.HasPrefix(u, "turns:") {
				urls = append(urls, rewriteHost(u, hostOverride))
			}
		}
		if len(urls) > 0 {
			out = append(out, webrtc.ICEServer{URLs: urls, Username: s.Username, Credential: s.Credential})
		}
	}
	if len(out) == 0 {
		return nil, errors.New("the server offers no TURN relay (is the built-in relay enabled, and may this member join voice?)")
	}
	return out, nil
}

// rewriteHost replaces the host of a turn: or turns: URL ("turn:host:3479?transport=udp").
func rewriteHost(u, host string) string {
	if host == "" {
		return u
	}
	scheme, rest, ok := strings.Cut(u, ":")
	if !ok {
		return u
	}
	hostport, query, hasQuery := strings.Cut(rest, "?")
	_, port, err := net.SplitHostPort(hostport)
	if err != nil {
		port = ""
	}
	out := scheme + ":" + host
	if port != "" {
		out += ":" + port
	}
	if hasQuery {
		out += "?" + query
	}
	return out
}
