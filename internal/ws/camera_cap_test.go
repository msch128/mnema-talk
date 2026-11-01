package ws

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
)

func TestParseCameraBatch(t *testing.T) {
	viewer, a, b := uuid.New(), uuid.New(), uuid.New()
	body := func(v any) json.RawMessage {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}

	ids, on, batch, ok := parseCameraBatch(body(map[string]any{"kind": "camera", "user_ids": []string{a.String(), b.String()}, "on": true}), viewer)
	if !batch || !ok || !on || len(ids) != 2 || ids[0] != a || ids[1] != b {
		t.Fatalf("a valid batch: %v %v %v %v", ids, on, batch, ok)
	}

	// The single-publisher forms are not batches.
	for _, raw := range []json.RawMessage{
		body(map[string]any{"kind": "camera", "user_id": a.String(), "on": true}),
		body(map[string]any{"kind": "camera", "all": true, "on": false}),
		json.RawMessage(`not json`),
	} {
		if _, _, batch, _ := parseCameraBatch(raw, viewer); batch {
			t.Fatalf("%s is not a batch", raw)
		}
	}

	many := make([]string, maxCameraBatch+1)
	for i := range many {
		many[i] = uuid.NewString()
	}
	for name, v := range map[string]map[string]any{
		"empty":       {"kind": "camera", "user_ids": []string{}, "on": true},
		"too many":    {"kind": "camera", "user_ids": many, "on": true},
		"bad id":      {"kind": "camera", "user_ids": []string{a.String(), "nope"}, "on": true},
		"self":        {"kind": "camera", "user_ids": []string{viewer.String()}, "on": true},
		"screen kind": {"kind": "screen", "user_ids": []string{a.String()}, "on": true},
	} {
		if _, _, batch, ok := parseCameraBatch(body(v), viewer); !batch || ok {
			t.Fatalf("%s must be dropped as a malformed batch (batch=%v ok=%v)", name, batch, ok)
		}
	}
}

func TestCappedCamerasGoToTheViewerOnly(t *testing.T) {
	h, _ := newSFUHub(t)
	ch := voiceCh()
	viewer := testClient(h, auth.User{ID: uuid.New(), Username: "viewer"})
	member := testClient(h, auth.User{ID: uuid.New(), Username: "member"})
	h.joinVoice(viewer, ch)
	h.joinVoice(member, ch)
	hidden := uuid.New()

	h.announceCamerasCapped(ch.ID, viewer.User.ID, []uuid.UUID{hidden})
	raw, ok := nextEventOf(viewer, "cameras_capped", viewersWait)
	if !ok || string(raw) != `{"channel_id":"`+ch.ID.String()+`","user_ids":["`+hidden.String()+`"]}` {
		t.Fatalf("the viewer was told %s (%v)", raw, ok)
	}
	h.announceCamerasCapped(ch.ID, viewer.User.ID, nil)
	raw, _ = nextEventOf(viewer, "cameras_capped", viewersWait)
	if string(raw) != `{"channel_id":"`+ch.ID.String()+`","user_ids":[]}` {
		t.Fatalf("nothing capped must be an empty array: %s", raw)
	}
	if _, ok := nextEventOf(member, "cameras_capped", quietWait); ok {
		t.Fatal("another member heard whose cameras were capped")
	}
}

func TestAViewerTurnsSeveralCamerasOffAndOnWithOneEvent(t *testing.T) {
	h, voiceSFU := newSFUHub(t)
	ch := voiceCh()
	a := testClient(h, auth.User{ID: uuid.New(), Username: "a"})
	b := testClient(h, auth.User{ID: uuid.New(), Username: "b"})
	viewer := testClient(h, auth.User{ID: uuid.New(), Username: "viewer"})
	for _, c := range []*Client{a, b, viewer} {
		h.joinVoice(c, ch)
	}
	room := voiceSFU.Room(ch.ID)
	if !room.ReceivesCamera(viewer.User.ID, a.User.ID) || !room.ReceivesCamera(viewer.User.ID, b.User.ID) {
		t.Fatal("cameras are on by default")
	}

	send := func(on bool, ids ...uuid.UUID) {
		payload, _ := json.Marshal(map[string]any{"kind": "camera", "user_ids": ids, "on": on})
		viewer.handle("webrtc_subscribe", payload)
	}
	send(false, a.User.ID, b.User.ID)
	if room.ReceivesCamera(viewer.User.ID, a.User.ID) || room.ReceivesCamera(viewer.User.ID, b.User.ID) {
		t.Fatal("both cameras are off after the batch")
	}
	send(true, a.User.ID)
	if !room.ReceivesCamera(viewer.User.ID, a.User.ID) || room.ReceivesCamera(viewer.User.ID, b.User.ID) {
		t.Fatal("only a is back")
	}

	// A malformed batch changes nothing, and a connection outside a call is ignored.
	send(false, uuid.Nil)
	if !room.ReceivesCamera(viewer.User.ID, a.User.ID) {
		t.Fatal("a malformed batch must be dropped")
	}
	ghost := testClient(h, auth.User{ID: uuid.New(), Username: "ghost"})
	payload, _ := json.Marshal(map[string]any{"kind": "camera", "user_ids": []uuid.UUID{a.User.ID}, "on": false})
	ghost.handle("webrtc_subscribe", payload)
	if !room.ReceivesCamera(viewer.User.ID, a.User.ID) {
		t.Fatal("a ghost changed someone's subscriptions")
	}
}
