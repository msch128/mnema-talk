package ws

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/perm/permtest"
	"github.com/msch128/mnema-talk/internal/sfu"
)

// What the hub does when one of the SFU's caps closes a member's media
// connection (H-L8-09). The sender cap is a condition honest use can reach:
// the member's socket is closed (close code closeRenewMedia), its seat waits
// out the grace period, and the client's resume join (rejoinAfterReconnect)
// comes back over a fresh media connection with nobody seeing it leave. An
// abuse cap ends the member's voice with "connection_lost", so the seat is
// never left silent.

type renewTalk struct {
	h      *Hub
	talk   *core.ChannelInfo
	member *Client
	other  *Client
}

func newRenewTalk(t *testing.T) renewTalk {
	t.Helper()
	f := permtest.NewFixture()
	svc := perm.NewServiceFrom(func(context.Context, time.Time) (*perm.Snapshot, error) { return f.Snapshot(), nil })
	h := NewHub(nil, nil, nil, nil, svc)
	t.Cleanup(h.Close)
	h.VoiceGrace = time.Minute
	media, err := sfu.NewSFU(0, 0, []string{"127.0.0.1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = media.Close() })
	h.SFU = media
	talk := &core.ChannelInfo{ID: f.Channel(uuid.Nil), Type: core.ChannelTypeVoice}
	rt := renewTalk{h: h, talk: talk,
		member: testClient(h, auth.User{ID: uuid.New()}), other: testClient(h, auth.User{ID: uuid.New()})}
	h.joinVoice(rt.other, talk)
	h.joinVoice(rt.member, talk)
	if rt.member.peer() == nil || !inRoom(h, talk.ID, rt.member.User.ID) {
		t.Fatal("no media connection after joining")
	}
	drainTypes(t, rt.other)
	drainTypes(t, rt.member)
	return rt
}

func waitHub(t *testing.T, label string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); !cond(); time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", label)
		}
	}
}

// kickReasons are the reasons of the voice_kicked events in c's queue.
func kickReasons(t *testing.T, c *Client) []string {
	t.Helper()
	var out []string
	for len(c.send) > 0 {
		var ev struct {
			Type    string `json:"type"`
			Payload struct {
				Reason string `json:"reason"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(<-c.send, &ev); err != nil {
			t.Fatal(err)
		}
		if ev.Type == "voice_kicked" {
			out = append(out, ev.Payload.Reason)
		}
	}
	return out
}

func TestSenderCapRenewsTheMembersConnection(t *testing.T) {
	rt := newRenewTalk(t)
	old := rt.member.peer()
	old.TripCap(true)
	waitHub(t, "the member's socket closed for a renewal", rt.member.closed.Load)
	if got := kickReasons(t, rt.member); len(got) != 0 {
		t.Fatalf("a renewed member was kicked: %v", got)
	}
	// What the closed socket's read loop does, then the client's reconnect
	// and resume join.
	rt.h.unregister(rt.member)
	if !inRoom(rt.h, rt.talk.ID, rt.member.User.ID) {
		t.Fatal("the member lost the seat during the renewal")
	}
	back := testClient(rt.h, rt.member.User)
	rt.h.resumeVoice(back, rt.talk)
	if back.peer() == nil || back.peer() == old {
		t.Fatal("the resumed member has no fresh media connection")
	}
	if n := count(drainTypes(t, rt.other), "voice_state_update"); n != 0 {
		t.Fatalf("the others saw %d join or leave events for a renewal", n)
	}
}

func TestAbuseCapEndsTheMembersVoice(t *testing.T) {
	rt := newRenewTalk(t)
	rt.member.peer().TripCap(false)
	waitHub(t, "the member's seat freed", func() bool { return !inRoom(rt.h, rt.talk.ID, rt.member.User.ID) })
	if got := kickReasons(t, rt.member); len(got) != 1 || got[0] != "connection_lost" {
		t.Fatalf("voice_kicked reasons %v, want [connection_lost]", got)
	}
	if rt.member.closed.Load() {
		t.Fatal("an abuse cap closed the member's socket")
	}
}
