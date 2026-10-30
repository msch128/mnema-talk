package main

import (
	"encoding/json"
	"testing"
)

func TestRewriteHostKeepsPortAndQuery(t *testing.T) {
	for in, want := range map[string]string{
		"turn:talk.example.org:3479?transport=udp": "turn:127.0.0.1:3479?transport=udp",
		"turns:talk.example.org:5349":              "turns:127.0.0.1:5349",
		"turn:1.2.3.4":                             "turn:127.0.0.1",
		"turn:[2001:db8::1]:3479?transport=tcp":    "turn:127.0.0.1:3479?transport=tcp",
	} {
		if got := rewriteHost(in, "127.0.0.1"); got != want {
			t.Errorf("rewriteHost(%q) = %q, want %q", in, got, want)
		}
		if got := rewriteHost(in, ""); got != in {
			t.Errorf("without an override %q became %q", in, got)
		}
	}
}

func TestTurnServersKeepOnlyRelaysWithTheirCredentials(t *testing.T) {
	var wire iceConfigWire
	raw := `{"ice_servers":[
	  {"urls":["stun:stun.example.org:3478"]},
	  {"urls":["turn:talk.example.org:3479?transport=udp","stun:x:1"],"username":"u","credential":"c"}]}`
	if err := json.Unmarshal([]byte(raw), &wire); err != nil {
		t.Fatal(err)
	}
	got, err := turnServers(wire, "")
	if err != nil || len(got) != 1 {
		t.Fatalf("got %+v, err %v", got, err)
	}
	if got[0].URLs[0] != "turn:talk.example.org:3479?transport=udp" || got[0].Username != "u" || got[0].Credential != "c" || len(got[0].URLs) != 1 {
		t.Fatalf("server = %+v", got[0])
	}
}

func TestNoRelayIsAnErrorNotAnEmptyDirectRun(t *testing.T) {
	var wire iceConfigWire
	_ = json.Unmarshal([]byte(`{"ice_servers":[{"urls":["stun:s:1"]}]}`), &wire)
	if _, err := turnServers(wire, ""); err == nil {
		t.Fatal("a configuration without TURN was accepted for relay mode")
	}
}
