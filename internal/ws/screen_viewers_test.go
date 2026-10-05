package ws

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/sfu"
)

type viewersPayload struct {
	ChannelID uuid.UUID   `json:"channel_id"`
	UserID    uuid.UUID   `json:"user_id"`
	Viewers   []uuid.UUID `json:"viewers"`
}

// SFU announcements arrive from signaling goroutines, so tests wait for them.
const (
	viewersWait = 3 * time.Second
	quietWait   = 200 * time.Millisecond
)

// nextViewers waits up to within for c's next screen_viewers event, skipping
// other events. It insists on a JSON array, never null.
func nextViewers(t *testing.T, c *Client, within time.Duration) (viewersPayload, bool) {
	t.Helper()
	timeout := time.After(within)
	for {
		select {
		case raw := <-c.send:
			var ev struct {
				Type    string                     `json:"type"`
				Payload map[string]json.RawMessage `json:"payload"`
			}
			if err := json.Unmarshal(raw, &ev); err != nil {
				t.Fatal(err)
			}
			if ev.Type != "screen_viewers" {
				continue
			}
			if v := string(ev.Payload["viewers"]); len(v) == 0 || v[0] != '[' {
				t.Fatalf("viewers must be a JSON array, got %s", v)
			}
			var p viewersPayload
			if err := json.Unmarshal(raw, &struct {
				Payload *viewersPayload `json:"payload"`
			}{&p}); err != nil {
				t.Fatal(err)
			}
			return p, true
		case <-timeout:
			return viewersPayload{}, false
		}
	}
}

func expectViewers(t *testing.T, c *Client, chID, sharer uuid.UUID, want ...uuid.UUID) {
	t.Helper()
	p, ok := nextViewers(t, c, viewersWait)
	if !ok {
		t.Fatalf("%s got no screen_viewers, want %v", c.User.Username, want)
	}
	if want == nil {
		want = []uuid.UUID{}
	}
	if p.ChannelID != chID || p.UserID != sharer || !slices.Equal(p.Viewers, want) {
		t.Fatalf("%s got %+v, want channel %s sharer %s viewers %v", c.User.Username, p, chID, sharer, want)
	}
}

func expectNoViewers(t *testing.T, cs ...*Client) {
	t.Helper()
	for _, c := range cs {
		if p, ok := nextViewers(t, c, quietWait); ok {
			t.Fatalf("%s got an unexpected screen_viewers %+v", c.User.Username, p)
		}
	}
}

func subscribeScreen(c *Client, sharer uuid.UUID, on bool) {
	payload, _ := json.Marshal(map[string]any{"kind": "screen", "user_id": sharer, "on": on})
	c.handle("webrtc_subscribe", payload)
}

func newSFUHub(t *testing.T) (*Hub, *sfu.SFU) {
	t.Helper()
	voiceSFU, err := sfu.NewSFU(0, 0, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return NewHub(nil, nil, voiceSFU, nil), voiceSFU
}

// startShare publishes a (media-less) screen share of c in its room and
// waits until the room heard about it.
func startShare(t *testing.T, voiceSFU *sfu.SFU, chID uuid.UUID, c *Client, roomMembers ...*Client) {
	t.Helper()
	room := voiceSFU.Room(chID)
	if room == nil {
		t.Fatal("no SFU room")
	}
	if err := room.PublishTestVideo(c.User.ID, sfu.SourceScreen); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(viewersWait)
	for !room.MediaStates()[c.User.ID].Screen {
		if time.Now().After(deadline) {
			t.Fatal("share never became live")
		}
		time.Sleep(5 * time.Millisecond)
	}
	// Let the start announcement go out, then forget it.
	time.Sleep(quietWait)
	for _, m := range roomMembers {
		drainTypes(t, m)
	}
}

func TestScreenViewersGoToTheRoomOnly(t *testing.T) {
	h, voiceSFU := newSFUHub(t)
	ch, other := voiceCh(), voiceCh()
	sharer := testClient(h, auth.User{ID: uuid.New(), Username: "sharer"})
	viewerUser := auth.User{ID: uuid.New(), Username: "viewer"}
	tab1, tab2 := testClient(h, viewerUser), testClient(h, viewerUser)
	member := testClient(h, auth.User{ID: uuid.New(), Username: "member"})
	outsider := testClient(h, auth.User{ID: uuid.New(), Username: "outsider"})
	lobby := testClient(h, auth.User{ID: uuid.New(), Username: "lobby"})
	for _, c := range []*Client{sharer, tab1, tab2, member} {
		h.joinVoice(c, ch)
	}
	h.joinVoice(outsider, other)
	inRoom := []*Client{sharer, tab1, tab2, member}
	startShare(t, voiceSFU, ch.ID, sharer, append(inRoom, outsider, lobby)...)

	subscribeScreen(tab1, sharer.User.ID, true)
	for _, c := range inRoom {
		expectViewers(t, c, ch.ID, sharer.User.ID, viewerUser.ID)
	}
	expectNoViewers(t, outsider, lobby)

	// The same user watching from a second tab is still one viewer.
	subscribeScreen(tab2, sharer.User.ID, true)
	expectNoViewers(t, inRoom...)

	// A joiner learns the audience of every live share, nobody else hears it again.
	late := testClient(h, auth.User{ID: uuid.New(), Username: "late"})
	drainTypes(t, late)
	h.joinVoice(late, ch)
	expectViewers(t, late, ch.ID, sharer.User.ID, viewerUser.ID)
	expectNoViewers(t, late, sharer, member, outsider, lobby)

	// A kick ends the viewer's subscription on every tab. (A tab still in
	// the room while the other is kicked may hear it too.)
	h.KickFromVoice(viewerUser.ID)
	for _, c := range []*Client{sharer, member, late} {
		expectViewers(t, c, ch.ID, sharer.User.ID)
	}
	expectNoViewers(t, sharer, member, late, outsider, lobby)
}

func TestScreenViewersClearedWhenSharerStops(t *testing.T) {
	h, voiceSFU := newSFUHub(t)
	ch := voiceCh()
	sharer := testClient(h, auth.User{ID: uuid.New(), Username: "sharer"})
	viewer := testClient(h, auth.User{ID: uuid.New(), Username: "viewer"})
	h.joinVoice(sharer, ch)
	h.joinVoice(viewer, ch)
	startShare(t, voiceSFU, ch.ID, sharer, sharer, viewer)

	subscribeScreen(viewer, sharer.User.ID, true)
	expectViewers(t, sharer, ch.ID, sharer.User.ID, viewer.User.ID)
	expectViewers(t, viewer, ch.ID, sharer.User.ID, viewer.User.ID)

	sharer.handle("webrtc_screenshare_stop", nil)
	expectViewers(t, sharer, ch.ID, sharer.User.ID)
	expectViewers(t, viewer, ch.ID, sharer.User.ID)
	expectNoViewers(t, sharer, viewer)

	// A joiner after the share ended hears nothing about it.
	late := testClient(h, auth.User{ID: uuid.New(), Username: "late"})
	h.joinVoice(late, ch)
	expectNoViewers(t, late)
}

func TestScreenViewersFollowLeavesAndDisconnects(t *testing.T) {
	h, voiceSFU := newSFUHub(t)
	h.VoiceGrace = time.Hour
	ch := voiceCh()
	sharer := testClient(h, auth.User{ID: uuid.New(), Username: "sharer"})
	leaver := testClient(h, auth.User{ID: uuid.New(), Username: "leaver"})
	dropper := testClient(h, auth.User{ID: uuid.New(), Username: "dropper"})
	for _, c := range []*Client{sharer, leaver, dropper} {
		h.joinVoice(c, ch)
	}
	startShare(t, voiceSFU, ch.ID, sharer, sharer, leaver, dropper)

	subscribeScreen(leaver, sharer.User.ID, true)
	expectViewers(t, sharer, ch.ID, sharer.User.ID, leaver.User.ID)
	subscribeScreen(dropper, sharer.User.ID, true)
	both := []uuid.UUID{leaver.User.ID, dropper.User.ID}
	slices.SortFunc(both, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })
	expectViewers(t, sharer, ch.ID, sharer.User.ID, both...)

	h.leaveCurrentVoice(leaver)
	expectViewers(t, sharer, ch.ID, sharer.User.ID, dropper.User.ID)

	// A dropped socket stops watching at once, even while its presence lingers.
	h.unregister(dropper)
	expectViewers(t, sharer, ch.ID, sharer.User.ID)
	expectNoViewers(t, sharer)
}

// Closing a voice channel kicks everyone; nobody outside hears about its shares.
func TestScreenViewersNotLeakedWhenChannelCloses(t *testing.T) {
	h, voiceSFU := newSFUHub(t)
	ch := voiceCh()
	sharer := testClient(h, auth.User{ID: uuid.New(), Username: "sharer"})
	viewer := testClient(h, auth.User{ID: uuid.New(), Username: "viewer"})
	lobby := testClient(h, auth.User{ID: uuid.New(), Username: "lobby"})
	h.joinVoice(sharer, ch)
	h.joinVoice(viewer, ch)
	startShare(t, voiceSFU, ch.ID, sharer, sharer, viewer, lobby)
	subscribeScreen(viewer, sharer.User.ID, true)
	expectViewers(t, sharer, ch.ID, sharer.User.ID, viewer.User.ID)
	expectViewers(t, viewer, ch.ID, sharer.User.ID, viewer.User.ID)
	expectNoViewers(t, lobby)

	h.CloseVoiceChannel(ch.ID)
	if voiceSFU.Room(ch.ID) != nil {
		t.Fatal("room still open")
	}
	expectNoViewers(t, lobby)
	// Whoever is kicked last may still hear the audience shrink, never grow.
	for _, c := range []*Client{sharer, viewer} {
		for {
			p, ok := nextViewers(t, c, quietWait)
			if !ok {
				break
			}
			if len(p.Viewers) != 0 {
				t.Fatalf("%s got %+v while the channel closed", c.User.Username, p)
			}
		}
	}
}
