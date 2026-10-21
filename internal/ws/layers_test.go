package ws

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/sfu"
)

// nextEventOf waits for c's next event of the given type, skipping others.
func nextEventOf(c *Client, typ string, within time.Duration) (json.RawMessage, bool) {
	timeout := time.After(within)
	for {
		select {
		case raw := <-c.send:
			var ev struct {
				Type    string          `json:"type"`
				Payload json.RawMessage `json:"payload"`
			}
			if json.Unmarshal(raw, &ev) == nil && ev.Type == typ {
				return ev.Payload, true
			}
		case <-timeout:
			return nil, false
		}
	}
}

type layersEvent struct {
	ChannelID uuid.UUID   `json:"channel_id"`
	UserID    uuid.UUID   `json:"user_id"`
	Layers    []sfu.Layer `json:"layers"`
}

func sendLayers(c *Client, layers any) {
	payload, _ := json.Marshal(map[string]any{"layers": layers})
	c.handle("webrtc_layers", payload)
}

func TestScreenLayersAreAnnouncedToTheRoom(t *testing.T) {
	h, voiceSFU := newSFUHub(t)
	ch, other := voiceCh(), voiceCh()
	sharer := testClient(h, auth.User{ID: uuid.New(), Username: "sharer"})
	viewer := testClient(h, auth.User{ID: uuid.New(), Username: "viewer"})
	outsider := testClient(h, auth.User{ID: uuid.New(), Username: "outsider"})
	h.joinVoice(sharer, ch)
	h.joinVoice(viewer, ch)
	h.joinVoice(outsider, other)
	startShare(t, voiceSFU, ch.ID, sharer, sharer, viewer, outsider)

	// Nonsense is ignored.
	sendLayers(sharer, []sfu.Layer{{Width: 100, Height: 100, FPS: 0}})
	sharer.handle("webrtc_layers", json.RawMessage(`not json`))
	if _, ok := nextEventOf(viewer, "screen_layers", quietWait); ok {
		t.Fatal("invalid layers were announced")
	}

	want := []sfu.Layer{{Width: 3840, Height: 2160, FPS: 60}, {Width: 1920, Height: 1080, FPS: 30}, {Width: 1280, Height: 720, FPS: 30}}
	sendLayers(sharer, want)
	for _, c := range []*Client{sharer, viewer} {
		raw, ok := nextEventOf(c, "screen_layers", viewersWait)
		if !ok {
			t.Fatalf("%s heard nothing", c.User.Username)
		}
		var ev layersEvent
		if err := json.Unmarshal(raw, &ev); err != nil {
			t.Fatal(err)
		}
		if ev.ChannelID != ch.ID || ev.UserID != sharer.User.ID {
			t.Fatalf("%s channel/user mismatch: %+v", c.User.Username, ev)
		}
		if len(ev.Layers) != 3 || ev.Layers[2] != want[2] {
			t.Fatalf("%s layers mismatch: %+v", c.User.Username, ev)
		}
	}
	if _, ok := nextEventOf(outsider, "screen_layers", quietWait); ok {
		t.Fatal("another room heard the layers")
	}

	// A joiner learns them.
	late := testClient(h, auth.User{ID: uuid.New(), Username: "late"})
	h.joinVoice(late, ch)
	raw, ok := nextEventOf(late, "screen_layers", viewersWait)
	if !ok {
		t.Fatal("a late joiner did not learn the layers")
	}
	var ev layersEvent
	if err := json.Unmarshal(raw, &ev); err != nil {
		t.Fatal(err)
	}
	if len(ev.Layers) != 3 {
		t.Fatalf("late joiner got %+v", ev)
	}

	// When the share ends the layers are withdrawn, as an array.
	sharer.handle("webrtc_screenshare_stop", nil)
	raw, ok = nextEventOf(viewer, "screen_layers", viewersWait)
	if !ok || string(raw) == "" {
		t.Fatal("the end of the share did not withdraw its layers")
	}
	var end struct {
		Layers []sfu.Layer `json:"layers"`
	}
	if err := json.Unmarshal(raw, &end); err != nil {
		t.Fatal(err)
	}
	if len(end.Layers) != 0 {
		t.Fatalf("withdrawn layers must be an empty array, got %s", raw)
	}
}

func TestLayersOutsideACallAreIgnored(t *testing.T) {
	h, _ := newSFUHub(t)
	c := testClient(h, auth.User{ID: uuid.New(), Username: "alone"})
	sendLayers(c, []sfu.Layer{{Width: 100, Height: 100, FPS: 30}}) // not in a room
	ch := voiceCh()
	h.joinVoice(c, ch)
	h.SFU.CloseRoom(ch.ID)
	sendLayers(c, []sfu.Layer{{Width: 100, Height: 100, FPS: 30}}) // room gone

	noSFU := NewHub(nil, nil, nil, nil)
	d := testClient(noSFU, auth.User{ID: uuid.New(), Username: "plain"})
	id := voiceCh().ID
	d.setVoice(&id)
	sendLayers(d, []sfu.Layer{{Width: 100, Height: 100, FPS: 30}})
}

func TestQualityAndWantedGoToOneMemberOfTheRoom(t *testing.T) {
	h, _ := newSFUHub(t)
	ch, other := voiceCh(), voiceCh()
	viewerUser := auth.User{ID: uuid.New(), Username: "viewer"}
	viewer, tab := testClient(h, viewerUser), testClient(h, viewerUser)
	sharer := testClient(h, auth.User{ID: uuid.New(), Username: "sharer"})
	member := testClient(h, auth.User{ID: uuid.New(), Username: "member"})
	h.joinVoice(viewer, ch)
	h.joinVoice(tab, other) // the same user's other tab sits in another room
	h.joinVoice(sharer, ch)
	h.joinVoice(member, ch)

	h.announceScreenQuality(ch.ID, viewer.User.ID, sharer.User.ID, sfu.QualityState{Layer: 1, Auto: true})
	raw, ok := nextEventOf(viewer, "screen_quality", viewersWait)
	if !ok {
		t.Fatal("the viewer was not told its quality")
	}
	var q struct {
		ChannelID uuid.UUID `json:"channel_id"`
		UserID    uuid.UUID `json:"user_id"`
		Layer     int       `json:"layer"`
		Auto      bool      `json:"auto"`
	}
	if err := json.Unmarshal(raw, &q); err != nil {
		t.Fatal(err)
	}
	if q.ChannelID != ch.ID || q.UserID != sharer.User.ID {
		t.Fatalf("quality event wrong channel or user: %+v", q)
	}
	if q.Layer != 1 || !q.Auto {
		t.Fatalf("quality event layer/auto mismatch: %+v", q)
	}
	for _, c := range []*Client{tab, sharer, member} {
		if _, ok := nextEventOf(c, "screen_quality", quietWait); ok {
			t.Fatalf("%s heard another member's quality", c.User.Username)
		}
	}

	h.announceLayersWanted(ch.ID, sharer.User.ID, []int{0, 2})
	raw, ok = nextEventOf(sharer, "screen_layers_wanted", viewersWait)
	if !ok {
		t.Fatal("the sharer was not told which layers are wanted")
	}
	var w struct {
		ChannelID uuid.UUID `json:"channel_id"`
		Layers    []int     `json:"layers"`
	}
	if err := json.Unmarshal(raw, &w); err != nil {
		t.Fatal(err)
	}
	if w.ChannelID != ch.ID {
		t.Fatalf("wanted event channel mismatch: %+v", w)
	}
	if len(w.Layers) != 2 || w.Layers[1] != 2 {
		t.Fatalf("wanted event layers mismatch: %+v", w)
	}
	h.announceLayersWanted(ch.ID, sharer.User.ID, nil)
	raw, _ = nextEventOf(sharer, "screen_layers_wanted", viewersWait)
	if string(raw) != `{"channel_id":"`+ch.ID.String()+`","layers":[]}` {
		t.Fatalf("no layers must be an empty array: %s", raw)
	}
	for _, c := range []*Client{viewer, member} {
		if _, ok := nextEventOf(c, "screen_layers_wanted", quietWait); ok {
			t.Fatalf("%s heard the sharer's wanted layers", c.User.Username)
		}
	}
}

// A browser sends the lower layers only where the server says it takes them.
func TestServerInfoAnnouncesScreenLayers(t *testing.T) {
	h, _ := newSFUHub(t)
	c := testClient(h, auth.User{ID: uuid.New(), Username: "member"})
	got := events(t, c, "server_info")
	if len(got) != 1 {
		t.Fatalf("expected 1 server_info event, got %d", len(got))
	}
	if got[0]["screen_layers"] != float64(sfu.MaxLayers) {
		t.Fatalf("server_info with media: %+v", got)
	}

	plain := NewHub(nil, nil, nil, nil)
	d := testClient(plain, auth.User{ID: uuid.New(), Username: "member"})
	got = events(t, d, "server_info")
	if len(got) != 1 {
		t.Fatalf("server_info without media: %+v", got)
	}
	if _, ok := got[0]["screen_layers"]; ok {
		t.Fatalf("a hub without media announced layers: %+v", got[0])
	}
}

func TestParseQuality(t *testing.T) {
	for _, c := range []struct {
		raw             string
		layer           int
		present, wantOK bool
	}{
		{``, 0, false, true},
		{`null`, 0, false, true},
		{`"auto"`, -1, true, true},
		{`"best"`, -1, true, false},
		{`0`, 0, true, true},
		{`2`, 2, true, true},
		{`3`, 0, true, false},
		{`-1`, 0, true, false},
		{`1.5`, 0, true, false},
		{`true`, 0, true, false},
	} {
		layer, present, ok := parseQuality(json.RawMessage(c.raw))
		if ok != c.wantOK {
			t.Errorf("%q: ok=%v, want %v", c.raw, ok, c.wantOK)
			continue
		}
		if present != c.present {
			t.Errorf("%q: present=%v, want %v", c.raw, present, c.present)
			continue
		}
		if ok && present && layer != c.layer {
			t.Errorf("%q: layer=%d, want %d", c.raw, layer, c.layer)
		}
	}
}

func TestSubscribeWithQuality(t *testing.T) {
	viewer, sharer := uuid.New(), uuid.New()
	parse := func(body string) (subscribeRequest, bool) {
		return parseSubscribe(json.RawMessage(body), viewer)
	}
	id := sharer.String()
	if req, ok := parse(`{"kind":"screen","user_id":"` + id + `","on":true,"quality":1}`); !ok {
		t.Errorf("screen with layer parse failed: %+v", req)
	} else if !req.hasQuality || req.quality != 1 {
		t.Errorf("screen with layer mismatch: %+v", req)
	}
	if req, ok := parse(`{"kind":"screen","user_id":"` + id + `","on":true,"quality":"auto"}`); !ok {
		t.Errorf("screen with auto parse failed: %+v", req)
	} else if !req.hasQuality || req.quality != -1 {
		t.Errorf("screen with auto mismatch: %+v", req)
	}
	if req, ok := parse(`{"kind":"screen","user_id":"` + id + `","on":true}`); !ok {
		t.Errorf("screen without quality parse failed: %+v", req)
	} else if req.hasQuality {
		t.Errorf("screen without quality had quality: %+v", req)
	}
	for _, body := range []string{
		`{"kind":"screen","user_id":"` + id + `","on":true,"quality":7}`,
		`{"kind":"screen","user_id":"` + id + `","on":true,"quality":"huge"}`,
		`{"kind":"camera","user_id":"` + id + `","on":true,"quality":1}`,
	} {
		if _, ok := parse(body); ok {
			t.Errorf("accepted %s", body)
		}
	}
}

func TestSubscribeChoosesTheLayerBeforeWatching(t *testing.T) {
	h, voiceSFU := newSFUHub(t)
	ch := voiceCh()
	sharer := testClient(h, auth.User{ID: uuid.New(), Username: "sharer"})
	viewer := testClient(h, auth.User{ID: uuid.New(), Username: "viewer"})
	h.joinVoice(sharer, ch)
	h.joinVoice(viewer, ch)
	startShare(t, voiceSFU, ch.ID, sharer, sharer, viewer)

	payload, _ := json.Marshal(map[string]any{"kind": "screen", "user_id": sharer.User.ID, "on": true, "quality": 2})
	viewer.handle("webrtc_subscribe", payload)
	// The share is watched by this viewer (the quality did not hinder it).
	expectViewers(t, sharer, ch.ID, sharer.User.ID, viewer.User.ID)

	// A connection without a media peer cannot choose.
	ghost := testClient(h, auth.User{ID: uuid.New(), Username: "ghost"})
	id := ch.ID
	ghost.setVoice(&id)
	payload, _ = json.Marshal(map[string]any{"kind": "screen", "user_id": sharer.User.ID, "on": true, "quality": 1})
	ghost.handle("webrtc_subscribe", payload)
}

func TestUnsendableAnnouncementsAreDropped(t *testing.T) {
	h, _ := newSFUHub(t)
	ch := voiceCh()
	c := testClient(h, auth.User{ID: uuid.New(), Username: "member"})
	h.joinVoice(c, ch)
	h.sendToVoiceUser(ch.ID, c.User.ID, "screen_quality", make(chan int))
	if _, ok := nextEventOf(c, "screen_quality", quietWait); ok {
		t.Fatal("an event that cannot be encoded was delivered")
	}
	// A missing list is sent as an empty one.
	h.announceScreenLayers(ch.ID, uuid.New(), nil)
	raw, ok := nextEventOf(c, "screen_layers", viewersWait)
	if !ok {
		t.Fatal("no announcement")
	}
	if !json.Valid(raw) {
		t.Fatalf("invalid announcement JSON: %s", raw)
	}
	var ev struct {
		Layers []sfu.Layer `json:"layers"`
	}
	if err := json.Unmarshal(raw, &ev); err != nil {
		t.Fatal(err)
	}
	if ev.Layers == nil {
		t.Fatalf("layers must be an array: %s", raw)
	}
}
