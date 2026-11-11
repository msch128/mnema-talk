package ws

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/events/eventstest"
	"github.com/msch128/mnema-talk/internal/sfu"
)

// wireHub records the frames one member's connection receives from the hub's
// own events (presence, Talks, media), byte for byte, into a golden file. See
// internal/events/eventstest.
type wireHub struct {
	t     *testing.T
	h     *Hub
	g     *eventstest.Golden
	watch *Client
}

// step records what the watching connection received since the last step.
func (w *wireHub) step(name string) {
	w.t.Helper()
	var frames []eventstest.Frame
	for len(w.watch.send) > 0 {
		frames = append(frames, eventstest.Frame{Wire: <-w.watch.send})
	}
	w.g.Step(name, frames...)
}

func wireMember(name string) auth.User {
	return auth.User{ID: uuid.New(), Username: name, DisplayName: name, Role: auth.RoleUser, CreatedAt: time.Now()}
}

func TestWireFormatOfPresenceAndTalkEvents(t *testing.T) {
	h := NewHub(nil, nil, nil, nil, openPerms())
	h.Version = "3.0.2"
	ch := voiceCh()
	me, other := wireMember("watcher"), wireMember("other")
	w := &wireHub{t: t, h: h, g: eventstest.Open(t, "event_wire_hub"), watch: testClient(h, me)}
	defer w.g.Check()
	w.step("greeting of a new connection")

	peer := testClient(h, other)
	w.step("another member comes online")
	h.SetPresence(other.ID, auth.PresenceDND)
	w.step("presence chosen")
	bot := uuid.New()
	h.SetBotPresence(bot, true)
	h.SetBotStatus(bot, "online", &BotActivity{Type: 0, Name: "chess"})
	w.step("bot presence with an activity")
	sendEvent(w.watch, "ping", map[string]any{"t": 1234})
	w.step("ping")
	sendEvent(peer, "typing", map[string]any{"channel_id": ch.ID})
	w.step("typing")

	h.joinVoice(w.watch, ch)
	h.joinVoice(peer, ch)
	w.step("joining a Talk")
	sendEvent(peer, "voice_speaking", map[string]any{"active": true})
	sendEvent(peer, "voice_mute_state", map[string]any{"muted": true, "deafened": false})
	w.step("speaking and mute state")
	sendEvent(peer, "talk_reaction", map[string]any{"emoji": "👏"})
	w.step("Talk reaction")
	w.mediaSteps(ch.ID, other.ID)
	h.KickFromVoice(other.ID)
	w.step("a member is removed from the Talk")
	h.kickClientPeer(w.watch, nil, "connection_lost", nil)
	w.step("own media connection lost")
	h.unregister(peer)
	w.step("a member goes offline")
}

func (w *wireHub) mediaSteps(ch, sharer uuid.UUID) {
	w.t.Helper()
	me := w.watch.User.ID
	w.h.announceMediaState(ch, sharer, sfu.MediaState{Screen: true})
	w.step("screen share starts")
	w.h.announceScreenViewers(ch, sharer, []uuid.UUID{me})
	w.h.announceScreenViewers(ch, sharer, nil)
	w.step("screen viewers")
	w.h.announceScreenLayers(ch, sharer, []sfu.Layer{{Width: 1920, Height: 1080, FPS: 30}})
	w.h.announceScreenLayers(ch, sharer, nil)
	w.step("screen layers")
	w.h.announceScreenQuality(ch, me, sharer, sfu.QualityState{Layer: 1, Auto: true})
	w.step("screen quality")
	w.h.announceLayersWanted(ch, me, []int{0, 2})
	w.h.announceLayersWanted(ch, me, nil)
	w.h.announceCameraLayersWanted(ch, me, []int{1})
	w.step("layers wanted")
	w.h.announceCamerasCapped(ch, me, []uuid.UUID{sharer})
	w.h.announceCamerasCapped(ch, me, nil)
	w.step("cameras capped")
}

func TestWireFormatOfStageEvents(t *testing.T) {
	s := newStageWorld(t)
	w := &wireHub{t: t, h: s.h, g: eventstest.Open(t, "event_wire_stage"), watch: s.owner}
	defer w.g.Check()
	sendEvent(s.insider, "stage_hand", map[string]any{"raised": true})
	w.step("hand raised")
	sendEvent(s.manager, "stage_talk_power", map[string]any{"user_id": s.insider.User.ID, "granted": true})
	w.step("talk power granted")
	sendEvent(s.manager, "stage_talk_power", map[string]any{"user_id": s.insider.User.ID, "granted": false})
	w.step("talk power taken")
	sendEvent(s.insider, "stage_hand", map[string]any{"raised": true})
	sendEvent(s.manager, "stage_hand", map[string]any{"raised": false, "user_id": s.insider.User.ID})
	w.step("hand raised and dismissed")
}
