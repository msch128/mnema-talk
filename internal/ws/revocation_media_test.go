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
	"github.com/pion/webrtc/v4"
)

// The hub's half of the revocation matrix for media, with a real SFU: each
// row takes one right from a seated member and checks what happens to their
// media connection. The SFU's half (that a closed connection neither hears
// nor is heard, and that a denied source stops) is measured over real
// connections in internal/sfu/revocation_media_test.go.
func TestRevokedTalkRightsReachTheMediaConnection(t *testing.T) {
	type outcome int
	const (
		closed outcome = iota // the media connection is closed and forgotten
		narrowed              // it stays, with these sources denied
	)
	rows := []struct {
		name   string
		revoke func(*permtest.Fixture, *Hub, uuid.UUID, uuid.UUID)
		want   outcome
		denied sfu.SourceSet
	}{
		{"loses Connect", func(f *permtest.Fixture, _ *Hub, ch, _ uuid.UUID) {
			f.RoleOverwrite(true, ch, f.EveryoneID(), 0, perm.Connect)
		}, closed, 0},
		{"Talk made private", func(f *permtest.Fixture, _ *Hub, ch, _ uuid.UUID) {
			f.RoleOverwrite(true, ch, f.EveryoneID(), 0, perm.ViewChannel)
		}, closed, 0},
		{"kicked from the Talk", func(_ *permtest.Fixture, h *Hub, ch, user uuid.UUID) { h.KickFromChannel(user, ch) }, closed, 0},
		// Session revoked, signed out, password changed, second factor reset:
		// the handlers close the user's sockets (DisconnectSession or
		// DisconnectUser); the media connection goes when the socket leaves the
		// hub, while the listing lingers for the grace period.
		{"socket closed by a sign-out", func(_ *permtest.Fixture, h *Hub, _, user uuid.UUID) {
			for _, c := range h.clientsOf(user) {
				h.DisconnectUser(user)
				h.unregister(c) // what the closed socket's read loop does
			}
		}, closed, 0},
		{"loses Speak", func(f *permtest.Fixture, _ *Hub, ch, _ uuid.UUID) {
			f.RoleOverwrite(true, ch, f.EveryoneID(), 0, perm.Speak)
		}, narrowed, sfu.SourceSet(0).With(sfu.SourceAudio).With(sfu.SourceScreenAudio)},
		{"loses Video (camera and screen)", func(f *permtest.Fixture, _ *Hub, ch, _ uuid.UUID) {
			f.RoleOverwrite(true, ch, f.EveryoneID(), 0, perm.Video)
		}, narrowed, sfu.SourceSet(0).With(sfu.SourceScreen).With(sfu.SourceScreenAudio).With(sfu.SourceCamera)},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			f := permtest.NewFixture()
			svc := perm.NewServiceFrom(func(context.Context, time.Time) (*perm.Snapshot, error) { return f.Snapshot(), nil })
			h := NewHub(nil, nil, nil, nil, svc)
			t.Cleanup(h.Close)
			media, err := sfu.NewSFU(0, 0, []string{"127.0.0.1"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = media.Close() })
			h.SFU = media
			talk := &core.ChannelInfo{ID: f.Channel(uuid.Nil), Type: core.ChannelTypeVoice}
			m := testClient(h, auth.User{ID: uuid.New()})
			h.joinVoice(m, talk)
			peer := m.peer()
			if peer == nil || !inRoom(h, talk.ID, m.User.ID) {
				t.Fatal("no media connection after joining")
			}

			row.revoke(f, h, talk.ID, m.User.ID)
			svc.Invalidate()
			start := time.Now()
			h.PermissionsChanged([]uuid.UUID{m.User.ID})
			took := time.Since(start)

			switch row.want {
			case closed:
				if st := peer.PC.ConnectionState(); st != webrtc.PeerConnectionStateClosed {
					t.Fatalf("the media connection is %v after the change", st)
				}
				if m.peer() != nil || m.activeVoiceRoom() != nil {
					t.Fatal("the connection still holds its media peer or room")
				}
				// What the client sends afterwards (an answer to renegotiate, a
				// layer switch or a subscription) finds no peer and no room.
				m.handleAnswer(json.RawMessage(`{"type":"answer","sdp":"v=0"}`))
				m.handleSubscribe(json.RawMessage(`{"kind":"screen","user_id":"` + uuid.NewString() + `","on":true,"quality":0}`))
			case narrowed:
				if peer.PC.ConnectionState() == webrtc.PeerConnectionStateClosed || m.peer() != peer {
					t.Fatal("a member who may still connect lost the media connection")
				}
				if got := peer.DeniedSources(); got&row.denied != row.denied {
					t.Fatalf("denied sources %v, want at least %v", got, row.denied)
				}
			}
			t.Logf("%s: enforced within %v", row.name, took.Round(time.Microsecond))
		})
	}
}
