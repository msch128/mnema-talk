package ws

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/sfu"
)

// stageWorld is the permission world with its public Talk made a Stage, a
// stage manager (a role holding only Manage stage) and a peer on every
// member's connection, so what the server lets a member publish can be read.
type stageWorld struct {
	*permWorld
	manager *Client
	peers   map[*Client]*sfu.Peer
}

func newStageWorld(t *testing.T) *stageWorld {
	t.Helper()
	w := &stageWorld{permWorld: newPermWorld(t), peers: map[*Client]*sfu.Peer{}}
	w.f.Stage(w.public.ID, true)
	w.f.Owner(w.owner.User.ID)
	role := w.f.Role(2, perm.ManageStage)
	user := auth.User{ID: uuid.New()}
	w.f.Assign(user.ID, role)
	w.manager = testClient(w.h, user)
	drainTypes(t, w.manager)
	for _, c := range []*Client{w.owner, w.manager, w.insider, w.outsider} {
		w.join(t, c)
	}
	return w
}

// join seats c in the Stage with a media peer whose denied sources the
// server decides, as it does for a real call.
func (w *stageWorld) join(t *testing.T, c *Client) {
	t.Helper()
	w.h.joinVoice(c, w.public)
	peer := &sfu.Peer{}
	c.setPeer(peer)
	w.peers[c] = peer
	w.h.applyMediaPolicy(c, w.f.Snapshot())
	drainTypes(t, c)
}

func (w *stageWorld) send(c *Client, eventType string, payload any) { sendEvent(c, eventType, payload) }

// sendEvent delivers a client event to c's handler as if it came off the wire.
func sendEvent(c *Client, eventType string, payload any) {
	raw, _ := json.Marshal(payload)
	c.handle(eventType, raw)
}

func (w *stageWorld) denied(c *Client) sfu.SourceSet { return w.peers[c].DeniedSources() }

func allMedia() sfu.SourceSet {
	return sfu.SourceSet(0).With(sfu.SourceAudio).With(sfu.SourceScreenAudio).With(sfu.SourceScreen).With(sfu.SourceCamera)
}

func TestStageRefusesMediaWithoutTalkPower(t *testing.T) {
	w := newStageWorld(t)
	if got := w.denied(w.outsider); got != allMedia() {
		t.Fatalf("an audience member may send %b", allMedia()&^got)
	}
	if got := w.denied(w.insider); got != allMedia() {
		t.Fatalf("an audience member may send %b", allMedia()&^got)
	}
	// Manage stage implies talk power, and so does the Owner.
	if got := w.denied(w.manager); got != 0 {
		t.Fatalf("a stage manager is denied %b", got)
	}
	if got := w.denied(w.owner); got != 0 {
		t.Fatalf("the Owner is denied %b", got)
	}
}

func TestStageTalkPowerIsGivenAndTaken(t *testing.T) {
	w := newStageWorld(t)
	user := w.outsider.User.ID
	w.send(w.manager, events.ClientStageTalkPower, map[string]any{"user_id": user, "granted": true})
	if got := w.denied(w.outsider); got != 0 {
		t.Fatalf("after the grant the member is still denied %b", got)
	}
	for _, c := range []*Client{w.manager, w.outsider, w.insider} {
		if count(drainTypes(t, c), events.StageTalkPower) != 1 {
			t.Fatal("everyone in the Talk is told of the grant")
		}
	}
	// Talk power does not outlast the stay, and never works around Speak.
	w.send(w.manager, events.ClientStageTalkPower, map[string]any{"user_id": user, "granted": false})
	if got := w.denied(w.outsider); got != allMedia() {
		t.Fatalf("after the revoke the member may send %b", allMedia()&^got)
	}
}

func TestStageTalkPowerNeedsManageStage(t *testing.T) {
	w := newStageWorld(t)
	user := w.outsider.User.ID
	// Neither an audience member, nor somebody holding another admin right.
	w.f.Everyone(perm.DefaultEveryone | perm.ManageChannels | perm.ManageMembers | perm.DisconnectMembers)
	w.send(w.insider, events.ClientStageTalkPower, map[string]any{"user_id": user, "granted": true})
	w.send(w.outsider, events.ClientStageTalkPower, map[string]any{"user_id": w.outsider.User.ID, "granted": true})
	if got := w.denied(w.outsider); got != allMedia() {
		t.Fatalf("a member without Manage stage handed out talk power: %b", got)
	}
	if got := drainTypes(t, w.manager); count(got, events.StageTalkPower) != 0 {
		t.Fatal("nothing is announced for a refused grant")
	}
	// Somebody who is not in the Talk cannot receive it.
	w.send(w.manager, events.ClientStageTalkPower, map[string]any{"user_id": uuid.New(), "granted": true})
	if got := drainTypes(t, w.manager); count(got, events.StageTalkPower) != 0 {
		t.Fatal("talk power for a stranger")
	}
}

func TestStageHandRaiseAcceptAndDismiss(t *testing.T) {
	w := newStageWorld(t)
	a, b := w.outsider, w.insider
	w.send(a, events.ClientStageHand, map[string]any{"raised": true})
	w.send(b, events.ClientStageHand, map[string]any{"raised": true})
	for _, c := range []*Client{w.manager, a, b} {
		if n := count(drainTypes(t, c), events.StageHand); n != 2 {
			t.Fatalf("%d hand events, want 2", n)
		}
	}
	// Raising twice is no news.
	w.send(a, events.ClientStageHand, map[string]any{"raised": true})
	if count(drainTypes(t, w.manager), events.StageHand) != 0 {
		t.Fatal("a hand already up was announced again")
	}
	// A member cannot dismiss another's hand; a manager can.
	w.send(b, events.ClientStageHand, map[string]any{"user_id": a.User.ID, "raised": false})
	if count(drainTypes(t, w.manager), events.StageHand) != 0 {
		t.Fatal("a member dismissed another member's hand")
	}
	w.send(w.manager, events.ClientStageHand, map[string]any{"user_id": b.User.ID, "raised": false})
	if count(drainTypes(t, w.manager), events.StageHand) != 1 {
		t.Fatal("the dismissal is announced")
	}
	// Accepting is the grant: the hand goes down by itself.
	w.send(w.manager, events.ClientStageTalkPower, map[string]any{"user_id": a.User.ID, "granted": true})
	got := drainTypes(t, w.manager)
	if count(got, events.StageHand) != 1 || count(got, events.StageTalkPower) != 1 {
		t.Fatalf("accepting a hand: %v", got)
	}
	if w.denied(a) != 0 {
		t.Fatal("the accepted member may not speak")
	}
	// Whoever may already speak has nothing to ask for.
	w.send(a, events.ClientStageHand, map[string]any{"raised": true})
	w.send(w.manager, events.ClientStageHand, map[string]any{"raised": true})
	if count(drainTypes(t, w.manager), events.StageHand) != 0 {
		t.Fatal("a speaker raised a hand")
	}
}

func TestStageHandNeedsSpeakAndAStage(t *testing.T) {
	w := newStageWorld(t)
	w.f.Everyone(perm.DefaultEveryone &^ perm.Speak)
	w.send(w.outsider, events.ClientStageHand, map[string]any{"raised": true})
	if count(drainTypes(t, w.manager), events.StageHand) != 0 {
		t.Fatal("a member who may never speak raised a hand")
	}
	w.f.Everyone(perm.DefaultEveryone)
	w.f.Stage(w.public.ID, false)
	w.send(w.outsider, events.ClientStageHand, map[string]any{"raised": true})
	w.send(w.manager, events.ClientStageTalkPower, map[string]any{"user_id": w.outsider.User.ID, "granted": true})
	if got := drainTypes(t, w.manager); count(got, events.StageHand)+count(got, events.StageTalkPower) != 0 {
		t.Fatalf("Stage commands in a normal Talk: %v", got)
	}
}

func TestStageStateEndsWithTheStay(t *testing.T) {
	w := newStageWorld(t)
	user := w.outsider.User.ID
	w.send(w.manager, events.ClientStageTalkPower, map[string]any{"user_id": user, "granted": true})
	w.send(w.insider, events.ClientStageHand, map[string]any{"raised": true})
	w.h.leaveCurrentVoice(w.outsider)
	w.h.leaveCurrentVoice(w.insider)
	w.join(t, w.outsider)
	w.join(t, w.insider)
	if w.denied(w.outsider) != allMedia() {
		t.Fatal("talk power survived leaving the Talk")
	}
	snap := w.h.voiceSnapshot()[w.public.ID]
	if snap[user].TalkPower || snap[w.insider.User.ID].HandRaisedAt != nil {
		t.Fatalf("a new stay starts clean: %+v", snap)
	}
	if !snap[w.manager.User.ID].TalkPower {
		t.Fatal("a stage manager shows as a speaker")
	}
}

func TestStageSnapshotCarriesTalkPowerAndHands(t *testing.T) {
	w := newStageWorld(t)
	w.send(w.manager, events.ClientStageTalkPower, map[string]any{"user_id": w.outsider.User.ID, "granted": true})
	w.send(w.insider, events.ClientStageHand, map[string]any{"raised": true})
	snap := w.h.voiceSnapshot()[w.public.ID]
	if !snap[w.outsider.User.ID].TalkPower || snap[w.insider.User.ID].HandRaisedAt == nil || snap[w.insider.User.ID].TalkPower {
		t.Fatalf("snapshot: %+v", snap)
	}
	raw, _ := json.Marshal(snap[w.insider.User.ID])
	var fields map[string]any
	_ = json.Unmarshal(raw, &fields)
	if _, ok := fields["hand_raised_at"]; !ok {
		t.Fatalf("hand_raised_at is not on the wire: %s", raw)
	}
}

func TestMakingATalkANormalTalkLiftsTheStage(t *testing.T) {
	w := newStageWorld(t)
	w.send(w.manager, events.ClientStageTalkPower, map[string]any{"user_id": w.outsider.User.ID, "granted": true})
	w.send(w.insider, events.ClientStageHand, map[string]any{"raised": true})
	drainTypes(t, w.insider)
	w.f.Stage(w.public.ID, false)
	w.h.PermissionsChanged(nil)
	if w.denied(w.insider) != 0 || w.denied(w.outsider) != 0 {
		t.Fatal("a normal Talk limits nobody")
	}
	got := drainTypes(t, w.insider)
	if count(got, events.StageHand) != 1 || count(got, events.StageTalkPower) < 1 {
		t.Fatalf("clients are told the Stage state is gone: %v", got)
	}
	// And a normal Talk made a Stage again starts from nothing.
	w.f.Stage(w.public.ID, true)
	w.h.PermissionsChanged(nil)
	if w.denied(w.outsider) != allMedia() {
		t.Fatal("old talk power came back with the Stage")
	}
}

func TestMakingATalkAStageSilencesTheAudienceAtOnce(t *testing.T) {
	w := newPermWorld(t)
	w.h.joinVoice(w.outsider, w.public)
	peer := &sfu.Peer{}
	w.outsider.setPeer(peer)
	w.h.PermissionsChanged(nil)
	if peer.DeniedSources() != 0 {
		t.Fatal("a normal Talk limits nobody")
	}
	w.f.Stage(w.public.ID, true)
	w.h.PermissionsChanged(nil)
	if peer.DeniedSources() != allMedia() {
		t.Fatalf("the audience may still send %b", allMedia()&^peer.DeniedSources())
	}
}

func TestManageStageChangesFollowThePermissionState(t *testing.T) {
	w := newStageWorld(t)
	role := w.f.Role(3, perm.ManageStage)
	w.f.Assign(w.outsider.User.ID, role)
	w.h.PermissionsChanged(nil)
	if w.denied(w.outsider) != 0 {
		t.Fatal("Manage stage implies talk power")
	}
	if !w.h.voiceSnapshot()[w.public.ID][w.outsider.User.ID].TalkPower {
		t.Fatal("the new manager shows as a speaker")
	}
	if count(drainTypes(t, w.insider), events.StageTalkPower) < 1 {
		t.Fatal("the Talk is told")
	}
	w.f.Unassign(w.outsider.User.ID, role)
	w.h.PermissionsChanged(nil)
	if w.denied(w.outsider) != allMedia() {
		t.Fatal("losing Manage stage takes the implied talk power")
	}
}
