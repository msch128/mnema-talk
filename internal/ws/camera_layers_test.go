package ws

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/sfu"
)

func TestCameraLayersAreDeclaredApartFromTheScreens(t *testing.T) {
	h, voiceSFU := newSFUHub(t)
	ch := voiceCh()
	sender := testClient(h, auth.User{ID: uuid.New(), Username: "sender"})
	viewer := testClient(h, auth.User{ID: uuid.New(), Username: "viewer"})
	h.joinVoice(sender, ch)
	h.joinVoice(viewer, ch)
	room := voiceSFU.Room(ch.ID)

	camera := []sfu.Layer{{Width: 1280, Height: 720, FPS: 30}, {Width: 640, Height: 360, FPS: 30}, {Width: 320, Height: 180, FPS: 15}}
	payload, _ := json.Marshal(map[string]any{"source": "camera", "layers": camera})
	sender.handle("webrtc_layers", payload)
	if got := room.CameraLayers(sender.User.ID); len(got) != 3 || got[2] != camera[2] {
		t.Fatalf("camera layers %+v", got)
	}
	if len(room.ScreenLayers()) != 0 {
		t.Fatal("camera layers were taken for the screen's")
	}
	if _, ok := nextEventOf(viewer, "screen_layers", quietWait); ok {
		t.Fatal("camera layers were announced to the room as a screen's")
	}

	// Nonsense for the camera is ignored, and a message without a source is still the screen's.
	bad, _ := json.Marshal(map[string]any{"source": "camera", "layers": []sfu.Layer{{Width: 10, Height: 10, FPS: 0}}})
	sender.handle("webrtc_layers", bad)
	if got := room.CameraLayers(sender.User.ID); len(got) != 3 {
		t.Fatalf("invalid camera layers replaced the declaration: %+v", got)
	}
	sendLayers(sender, camera[:2])
	if got := room.CameraLayers(sender.User.ID); len(got) != 3 {
		t.Fatalf("a screen declaration changed the camera's: %+v", got)
	}
}

func TestSubscribeWithCameraPictureSize(t *testing.T) {
	viewer, sharer := uuid.New(), uuid.New()
	parse := func(body string) (subscribeRequest, bool) {
		return parseSubscribe(json.RawMessage(body), viewer)
	}
	id := sharer.String()
	if req, ok := parse(`{"kind":"camera","user_id":"` + id + `","on":true,"fit_px":150}`); !ok || !req.hasFitPx || req.fitPx != 150 || req.hasFit {
		t.Errorf("camera with a size: %+v ok=%v", req, ok)
	}
	if req, ok := parse(`{"kind":"camera","user_id":"` + id + `","on":true}`); !ok || req.hasFitPx {
		t.Errorf("camera without a size: %+v ok=%v", req, ok)
	}
	for _, body := range []string{
		`{"kind":"camera","user_id":"` + id + `","on":true,"fit_px":-1}`,
		`{"kind":"camera","user_id":"` + id + `","on":true,"fit_px":99999}`,
		`{"kind":"camera","all":true,"on":true,"fit_px":100}`,
		`{"kind":"screen","user_id":"` + id + `","on":true,"fit_px":100}`,
	} {
		if _, ok := parse(body); ok {
			t.Errorf("accepted %s", body)
		}
	}
}

// A size report changes only what the viewer's picture needs: it neither
// subscribes nor un-hides anything.
func TestCameraPictureSizeReachesTheSFU(t *testing.T) {
	h, voiceSFU := newSFUHub(t)
	ch := voiceCh()
	sender := testClient(h, auth.User{ID: uuid.New(), Username: "sender"})
	viewer := testClient(h, auth.User{ID: uuid.New(), Username: "viewer"})
	h.joinVoice(sender, ch)
	h.joinVoice(viewer, ch)
	room := voiceSFU.Room(ch.ID)
	camera := []sfu.Layer{{Width: 1280, Height: 720, FPS: 30}, {Width: 640, Height: 360, FPS: 30}, {Width: 320, Height: 180, FPS: 15}}
	payload, _ := json.Marshal(map[string]any{"source": "camera", "layers": camera})
	sender.handle("webrtc_layers", payload)

	size, _ := json.Marshal(map[string]any{"kind": "camera", "user_id": sender.User.ID, "on": true, "fit_px": 150})
	viewer.handle("webrtc_subscribe", size)
	if got := room.CameraFit(viewer.User.ID, sender.User.ID); got != 2 {
		t.Fatalf("a 150 px picture fits layer %d, want 2", got)
	}
	big, _ := json.Marshal(map[string]any{"kind": "camera", "user_id": sender.User.ID, "on": true, "fit_px": 800})
	viewer.handle("webrtc_subscribe", big)
	if got := room.CameraFit(viewer.User.ID, sender.User.ID); got != 0 {
		t.Fatalf("an 800 px picture fits layer %d, want 0", got)
	}
	// Outside a call it is ignored.
	outsider := testClient(h, auth.User{ID: uuid.New(), Username: "outsider"})
	outsider.handle("webrtc_subscribe", size)
	if got := room.CameraFit(outsider.User.ID, sender.User.ID); got != 0 {
		t.Fatalf("a client outside the call set a size: %d", got)
	}
}

func TestCameraLayersWantedAreMarkedWithTheirSource(t *testing.T) {
	h, _ := newSFUHub(t)
	ch := voiceCh()
	sender := testClient(h, auth.User{ID: uuid.New(), Username: "sender"})
	h.joinVoice(sender, ch)

	h.announceCameraLayersWanted(ch.ID, sender.User.ID, []int{0, 2})
	raw, ok := nextEventOf(sender, "screen_layers_wanted", viewersWait)
	if !ok {
		t.Fatal("the sender was not told which camera layers are wanted")
	}
	if string(raw) != `{"channel_id":"`+ch.ID.String()+`","layers":[0,2],"source":"camera"}` {
		t.Fatalf("camera wanted payload: %s", raw)
	}
	h.announceCameraLayersWanted(ch.ID, sender.User.ID, nil)
	raw, _ = nextEventOf(sender, "screen_layers_wanted", viewersWait)
	if string(raw) != `{"channel_id":"`+ch.ID.String()+`","layers":[],"source":"camera"}` {
		t.Fatalf("no layers must be an empty array: %s", raw)
	}
}

func TestServerInfoAnnouncesCameraLayers(t *testing.T) {
	h, _ := newSFUHub(t)
	c := testClient(h, auth.User{ID: uuid.New(), Username: "member"})
	got := sentEvents(t, c, "server_info")
	if len(got) != 1 || got[0]["camera_layers"] != float64(sfu.MaxLayers) {
		t.Fatalf("server_info with media: %+v", got)
	}
	plain := NewHub(nil, nil, nil, nil)
	d := testClient(plain, auth.User{ID: uuid.New(), Username: "member"})
	got = sentEvents(t, d, "server_info")
	if _, ok := got[0]["camera_layers"]; len(got) != 1 || ok {
		t.Fatalf("a hub without media announced camera layers: %+v", got)
	}
}
