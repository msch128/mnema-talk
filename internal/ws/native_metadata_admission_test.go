package ws

import (
	"strings"
	"testing"
)

func TestNativeMetadataClosedFrames(t *testing.T) {
	cases := []struct {
		raw   string
		valid bool
	}{
		{`{"type":"ping","payload":{"t":42}}`, true},
		{`{"payload":{"idle":false},"type":"presence_idle"}`, true},
		{`{"type":"native_access_renew","payload":{"access_token":"` + strings.Repeat("A", 43) + `"}}`, true},
		{`{"type":"ping","type":"presence_idle","payload":{"idle":true}}`, false},
		{`{"type":"ping","payload":{"t":1,"t":2}}`, false},
		{`{"type":"ping","payload":{"t":1},"extra":true}`, false},
		{`{"type":"ping","payload":{"t":null}}`, false},
		{`{"type":"ping","payload":{"t":-1}}`, false},
		{`{"type":"ping","payload":{"t":1e100}}`, false},
		{`{"type":"ping","payload":{"unknown":1}}`, false},
		{`{"type":"presence_idle","payload":{"idle":null}}`, false},
		{`{"type":"presence_idle","payload":{"idle":"true"}}`, false},
		{`{"type":"presence_idle","payload":{"idle":true,"content":"blocked"}}`, false},
		{`{"type":"presence_idle","payload":null}`, false},
		{`{"type":"voice_join","payload":{"channel_id":"blocked"}}`, false},
		{`{"type":"native_unknown","payload":{}}`, false},
		{`{"type":"ping","payload":{"t":1}}{}`, false},
		{`[]`, false}, {`{`, false},
		{strings.Repeat(" ", 1025), false},
	}
	for i, c := range cases {
		_, _, valid := parseNativeMetadataFrame([]byte(c.raw))
		if valid != c.valid {
			t.Fatalf("closed metadata case %d mismatch", i)
		}
	}
}
func TestNativeMetadataOutgoingClosedEvents(t *testing.T) {
	for _, event := range []string{"server_info", "system_update", "pong", "presence_snapshot", "presence_update", "channels_changed", "member_joined", "user_update", "user_stats", "native_access_renewed"} {
		if !nativeMetadataEventAllowed([]byte(`{"type":"` + event + `","payload":null}`)) {
			t.Fatal("metadata event denied")
		}
	}
	for _, event := range []string{"message_create", "message_update", "message_delete", "reaction_update", "typing", "voice_snapshot", "webrtc_offer", "webrtc_candidate", "media_state", "screen_share", "native_unknown"} {
		if nativeMetadataEventAllowed([]byte(`{"type":"` + event + `","payload":null}`)) {
			t.Fatal("protected event allowed")
		}
	}
	if nativeMetadataEventAllowed([]byte(`{`)) {
		t.Fatal("invalid event allowed")
	}
}
