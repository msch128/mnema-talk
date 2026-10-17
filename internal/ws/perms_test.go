package ws

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/sfu"
	"github.com/pion/webrtc/v4"
)

// permWorld is a hub with a hand-built permission state: an Owner, an insider
// who holds the Staff role and an outsider, a public Talk and a private one.
type permWorld struct {
	h                             *Hub
	f                             *perm.Fixture
	owner, insider, outsider      *Client
	staff                         uuid.UUID
	public, private, privateVoice *chat.ChannelInfo
}

func newPermWorld(t *testing.T) *permWorld {
	t.Helper()
	f := perm.NewFixture()
	w := &permWorld{f: f, staff: f.Role(1, 0)}
	pub, priv, privVoice := f.Channel(uuid.Nil), f.Channel(uuid.Nil), f.Channel(uuid.Nil)
	for _, ch := range []uuid.UUID{priv, privVoice} {
		f.RoleOverwrite(true, ch, f.EveryoneID(), 0, perm.ViewChannel)
		f.RoleOverwrite(true, ch, w.staff, perm.ViewChannel, 0)
	}
	w.public = &chat.ChannelInfo{ID: pub, Type: chat.ChannelTypeVoice}
	w.private = &chat.ChannelInfo{ID: priv, Type: chat.ChannelTypeText}
	w.privateVoice = &chat.ChannelInfo{ID: privVoice, Type: chat.ChannelTypeVoice}
	w.h = NewHub(nil, nil, nil, nil)
	w.h.SetPermissions(f.Service())
	t.Cleanup(w.h.Close)
	ownerUser := auth.User{ID: uuid.New(), Role: auth.RoleAdmin}
	insiderUser, outsiderUser := auth.User{ID: uuid.New()}, auth.User{ID: uuid.New()}
	f.Assign(insiderUser.ID, w.staff)
	w.owner, w.insider, w.outsider = testClient(w.h, ownerUser), testClient(w.h, insiderUser), testClient(w.h, outsiderUser)
	for _, c := range []*Client{w.owner, w.insider, w.outsider} {
		drainTypes(t, c)
	}
	return w
}

func TestBroadcastChannelReachesViewersOnly(t *testing.T) {
	w := newPermWorld(t)
	w.h.BroadcastChannel(w.private.ID, "message_create", map[string]any{"x": 1})
	if count(drainTypes(t, w.owner), "message_create") != 1 || count(drainTypes(t, w.insider), "message_create") != 1 {
		t.Fatal("viewers must receive the event")
	}
	if got := drainTypes(t, w.outsider); len(got) != 0 {
		t.Fatalf("the outsider received %v", got)
	}
	w.h.BroadcastChannel(w.public.ID, "message_create", nil)
	if count(drainTypes(t, w.outsider), "message_create") != 1 {
		t.Fatal("an open channel reaches everybody")
	}
	// An unencodable payload is dropped like everywhere else.
	w.h.BroadcastChannel(w.public.ID, "bad", make(chan int))
	if got := drainTypes(t, w.outsider); len(got) != 0 {
		t.Fatalf("unencodable payload delivered: %v", got)
	}
}

func TestEventsFailClosedWhenPermissionsCannotBeLoaded(t *testing.T) {
	h := NewHub(nil, nil, nil, nil)
	h.SetPermissions(perm.NewService(nil)) // no database, no state
	t.Cleanup(h.Close)
	a, b := testClient(h, auth.User{ID: uuid.New()}), testClient(h, auth.User{ID: uuid.New()})
	// A fresh connection learns of no Talk and no media at all.
	if got := drainTypes(t, a); count(got, "voice_snapshot") != 1 {
		t.Fatalf("a connection still gets its snapshots: %v", got)
	}
	drainTypes(t, b)
	ch := uuid.New()
	h.BroadcastChannel(ch, "message_create", nil)
	a.emitApplicationEvent("typing", nil, true, nil, ch)
	h.PermissionsChanged(nil)
	if got := drainTypes(t, a); len(got) != 0 {
		t.Fatalf("events delivered without permissions: %v", got)
	}
	if got := drainTypes(t, b); len(got) != 0 {
		t.Fatalf("events delivered without permissions: %v", got)
	}
	// And a Talk cannot be joined.
	h.joinVoice(a, &chat.ChannelInfo{ID: ch, Type: chat.ChannelTypeVoice})
	if inRoom(h, ch, a.User.ID) || count(drainTypes(t, a), "voice_kicked") != 1 {
		t.Fatal("joined a Talk without permissions")
	}
}

func TestSnapshotsHideTalksFromThoseWhoCannotViewThem(t *testing.T) {
	w := newPermWorld(t)
	w.h.joinVoice(w.insider, w.privateVoice)
	w.h.joinVoice(w.owner, w.public)
	late := testClient(w.h, auth.User{ID: uuid.New()})
	// testClient registered it; read what the registration sent.
	var snapshot map[uuid.UUID]map[uuid.UUID]VoiceUser
	for len(late.send) > 0 {
		var ev struct {
			Type    string
			Payload json.RawMessage
		}
		if err := json.Unmarshal(<-late.send, &ev); err != nil {
			t.Fatal(err)
		}
		if ev.Type == "voice_snapshot" {
			if err := json.Unmarshal(ev.Payload, &snapshot); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, leaked := snapshot[w.privateVoice.ID]; leaked || len(snapshot[w.public.ID]) != 1 {
		t.Fatalf("snapshot for an outsider: %+v", snapshot)
	}
	rooms := w.h.voiceRooms(late, mustSnapshot(t, w.h))
	started := rooms["started"].(map[uuid.UUID]time.Time)
	if _, leaked := started[w.privateVoice.ID]; leaked || len(started) != 1 {
		t.Fatalf("voice_rooms for an outsider: %v", started)
	}
	if rooms := w.h.voiceRooms(w.owner, mustSnapshot(t, w.h)); len(rooms["started"].(map[uuid.UUID]time.Time)) != 2 {
		t.Fatal("the Owner sees every room")
	}
}

func TestJoinNeedsViewAndConnect(t *testing.T) {
	w := newPermWorld(t)
	w.h.joinVoice(w.outsider, w.privateVoice)
	if inRoom(w.h, w.privateVoice.ID, w.outsider.User.ID) || count(drainTypes(t, w.outsider), "voice_kicked") != 1 {
		t.Fatal("an outsider joined a private Talk")
	}
	// Connect denied for @everyone on the public Talk.
	w.f.RoleOverwrite(true, w.public.ID, w.f.EveryoneID(), 0, perm.Connect)
	w.h.joinVoice(w.outsider, w.public)
	if inRoom(w.h, w.public.ID, w.outsider.User.ID) {
		t.Fatal("joined without Connect")
	}
	w.h.joinVoice(w.owner, w.public)
	if !inRoom(w.h, w.public.ID, w.owner.User.ID) {
		t.Fatal("the Owner always connects")
	}
}

func TestPermissionsChangedRemovesWhoMayNoLongerConnect(t *testing.T) {
	w := newPermWorld(t)
	w.h.joinVoice(w.insider, w.privateVoice)
	w.h.joinVoice(w.outsider, w.public)
	drainTypes(t, w.insider)
	drainTypes(t, w.outsider)

	// Nothing changed: everybody stays, only a refresh is requested.
	w.h.PermissionsChanged(nil)
	if !inRoom(w.h, w.privateVoice.ID, w.insider.User.ID) || !inRoom(w.h, w.public.ID, w.outsider.User.ID) {
		t.Fatal("nobody should be removed while the permissions hold")
	}
	if count(drainTypes(t, w.insider), "permissions_changed") != 1 {
		t.Fatal("clients are told to refresh")
	}
	drainTypes(t, w.outsider)

	// The insider loses the role; only they are named, only they are removed.
	w.f.Unassign(w.insider.User.ID, w.staff)
	w.h.PermissionsChanged([]uuid.UUID{w.insider.User.ID})
	if inRoom(w.h, w.privateVoice.ID, w.insider.User.ID) {
		t.Fatal("the insider kept their seat in a Talk they can no longer view")
	}
	// The removal is immediate; the second refresh notice within a second is
	// held back and sent once when the second is over.
	if p := drainTypes(t, w.insider); count(p, "voice_kicked") != 1 || count(p, "permissions_changed") != 0 {
		t.Fatalf("the insider's events: %v", p)
	}
	time.Sleep(permNotifyEvery + 300*time.Millisecond)
	w.h.PermissionsChanged([]uuid.UUID{w.insider.User.ID})
	w.h.PermissionsChanged([]uuid.UUID{w.insider.User.ID})
	time.Sleep(permNotifyEvery + 300*time.Millisecond)
	if p := drainTypes(t, w.insider); count(p, "permissions_changed") != 2 {
		t.Fatalf("one immediate and one coalesced notice expected: %v", p)
	}
	if got := drainTypes(t, w.outsider); len(got) != 0 {
		t.Fatalf("an unnamed user was disturbed: %v", got)
	}
	if !inRoom(w.h, w.public.ID, w.outsider.User.ID) {
		t.Fatal("the outsider should still be in the public Talk")
	}
}

func TestKickUserFromChannelEndsALingeringSeat(t *testing.T) {
	w := newPermWorld(t)
	w.h.VoiceGrace = time.Minute
	w.h.joinVoice(w.insider, w.privateVoice)
	// The connection drops: the seat lingers for the grace period.
	w.h.unregister(w.insider)
	if !inRoom(w.h, w.privateVoice.ID, w.insider.User.ID) {
		t.Fatal("the seat should linger")
	}
	w.f.Unassign(w.insider.User.ID, w.staff)
	w.h.PermissionsChanged(nil)
	if inRoom(w.h, w.privateVoice.ID, w.insider.User.ID) {
		t.Fatal("a lingering seat in a Talk the user can no longer view must end")
	}
}

func TestMediaPolicyFollowsSpeakAndVideo(t *testing.T) {
	w := newPermWorld(t)
	c := w.outsider

	denied := func(p perm.Set) sfu.SourceSet {
		w.f.Everyone(p)
		return c.mediaDenied(w.f.Snapshot(), w.public.ID)
	}
	if got := denied(perm.DefaultEveryone); got != 0 {
		t.Fatalf("defaults deny nothing: %b", got)
	}
	if got := denied(perm.DefaultEveryone &^ perm.Speak); !got.Has(sfu.SourceAudio) || !got.Has(sfu.SourceScreenAudio) || got.Has(sfu.SourceCamera) || got.Has(sfu.SourceScreen) {
		t.Fatalf("no Speak: %b", got)
	}
	if got := denied(perm.DefaultEveryone &^ perm.Video); got.Has(sfu.SourceAudio) || !got.Has(sfu.SourceCamera) || !got.Has(sfu.SourceScreen) || !got.Has(sfu.SourceScreenAudio) {
		t.Fatalf("no Video: %b", got)
	}
	if got := c.mediaDenied(nil, w.public.ID); got != 0 {
		t.Fatal("no permission model, no limits")
	}
	w.f.Everyone(perm.DefaultEveryone)

	// Without a Talk or a peer there is nothing to apply.
	w.h.applyMediaPolicy(c, w.f.Snapshot())
	c.setVoice(&w.public.ID)
	w.h.applyMediaPolicy(c, w.f.Snapshot())

	// A peer without an SFU only learns what it may not send.
	peer := &sfu.Peer{}
	c.setPeer(peer)
	w.f.Everyone(perm.DefaultEveryone &^ perm.Speak)
	w.h.applyMediaPolicy(c, w.f.Snapshot())
	w.f.Everyone(perm.DefaultEveryone)

	// With a running room the denied sources are also unpublished.
	media, err := sfu.NewSFU(0, 0, []string{"127.0.0.1"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = media.Close() })
	w.h.SFU = media
	room, livePeer, err := media.Join(w.public.ID, c.User.ID, func(webrtc.SessionDescription) {}, func(*webrtc.ICECandidateInit) {})
	if err != nil {
		t.Fatal(err)
	}
	c.setPeer(livePeer)
	w.f.Everyone(perm.DefaultEveryone &^ perm.Speak &^ perm.Video)
	w.h.applyMediaPolicy(c, w.f.Snapshot())
	if room == nil {
		t.Fatal("no room")
	}
}

func mustSnapshot(t *testing.T, h *Hub) *perm.Snapshot {
	t.Helper()
	snap, err := h.Perms.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

// A change that lands between the first check and the seat is caught by the
// check after it.
func TestJoinIsRevokedWhenPermissionsChangeMeanwhile(t *testing.T) {
	w := newPermWorld(t)
	w.h.afterSeat = func() { w.f.RoleOverwrite(true, w.public.ID, w.f.EveryoneID(), 0, perm.Connect) }
	w.h.joinVoice(w.outsider, w.public)
	if inRoom(w.h, w.public.ID, w.outsider.User.ID) {
		t.Fatal("the seat survived the revocation")
	}
	if got := drainTypes(t, w.outsider); count(got, "voice_kicked") != 1 || count(got, "webrtc_offer") != 0 {
		t.Fatalf("events: %v", got)
	}
}
