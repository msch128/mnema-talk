package gateway

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParsePresence(t *testing.T) {
	ok := func(in string) Presence {
		t.Helper()
		p, err := parsePresence(json.RawMessage(in))
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		return p
	}
	bad := func(in string) {
		t.Helper()
		if _, err := parsePresence(json.RawMessage(in)); err == nil {
			t.Fatalf("%s was accepted", in)
		}
	}

	if p := ok(`{"status":"online"}`); p.Status != StatusOnline || p.Activity != nil {
		t.Fatalf("plain: %+v", p)
	}
	if p := ok(`{"status":"offline"}`); p.Status != StatusInvisible {
		t.Fatalf("offline is invisible: %+v", p)
	}
	p := ok(`{"since":null,"afk":false,"status":"dnd","activities":[{"name":"  a‮b \n c\u0000 ","type":0},{"name":"second","type":0}]}`)
	if p.Status != StatusDND || p.Activity == nil || p.Activity.Name != "ab c" || p.Activity.Type != ActivityPlaying {
		t.Fatalf("sanitized: %+v", p)
	}
	if p := ok(`{"status":"idle","activities":[{"name":"Custom Status","state":"out to lunch","type":4}]}`); p.Activity == nil || p.Activity.Name != "out to lunch" {
		t.Fatalf("custom status uses state: %+v", p)
	}
	if p := ok(`{"status":"online","activities":[{"name":"` + strings.Repeat("é", 300) + `","type":3}]}`); len([]rune(p.Activity.Name)) != MaxActivityLen {
		t.Fatalf("not limited: %d", len([]rune(p.Activity.Name)))
	}
	// An activity with nothing left after cleaning is none.
	if p := ok(`{"status":"online","activities":[{"name":"‮\n","type":0}]}`); p.Activity != nil {
		t.Fatalf("empty activity kept: %+v", p)
	}

	bad(`{}`)
	bad(`{"status":"busy"}`)
	bad(`{"status":"online","activities":[{"name":"x","type":1}]}`)
	bad(`{"status":"online","activities":[{"name":"x","type":-1}]}`)
	bad(`{"status":5}`)
	bad(`nope`)
}
