//go:build integration

package chat

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/testutil"
)

type talksWorld struct {
	p        *db.Pool
	rec      *events.Recorder
	mgr      *TalkManager
	category uuid.UUID
	lobby    *Channel
	alice    *auth.User
	bob      *auth.User
	closed   atomic.Int32
}

type voiceCloser struct{ w *talksWorld }

func (v voiceCloser) CloseVoiceChannel(uuid.UUID) { v.w.closed.Add(1) }

func newTalksUser(t *testing.T, p *db.Pool, name string) *auth.User {
	t.Helper()
	u := &auth.User{Username: name, DisplayName: name, Role: auth.RoleUser}
	if err := p.QueryRow(context.Background(), `INSERT INTO users (username, display_name, password_hash) VALUES ($1,$1,'x') RETURNING id, created_at`, name).Scan(&u.ID, &u.CreatedAt); err != nil {
		t.Fatal(err)
	}
	return u
}

func newTalksWorld(t *testing.T) *talksWorld {
	t.Helper()
	p := testutil.DB(t)
	testutil.Reset(t, p)
	ctx := context.Background()
	w := &talksWorld{p: p, rec: &events.Recorder{}}
	cat, err := CreateCategory(ctx, p, "Voice", 0)
	if err != nil {
		t.Fatal(err)
	}
	w.category = cat.ID
	w.lobby, err = CreateChannel(ctx, p, &cat.ID, "Create a Talk", ChannelTypeVoice, "", 5)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := UpdateChannel(ctx, p, w.lobby.ID, nil, nil, ptr(4), nil); err != nil {
		t.Fatal(err)
	}
	if err := SetLobby(ctx, p, w.lobby.ID, true); err != nil {
		t.Fatal(err)
	}
	w.alice, w.bob = newTalksUser(t, p, "alice_t"), newTalksUser(t, p, "bob_t")
	w.mgr = NewTalkManager(&TalkManager{
		DB: p, Events: w.rec, Perms: perm.NewService(p), Audit: &audit.Log{DB: p},
		Voice: voiceCloser{w}, EmptyGrace: 40 * time.Millisecond,
	})
	return w
}

func (w *talksWorld) join(ch, user uuid.UUID)  { w.mgr.apply(presenceEvent{user, ch, true}) }
func (w *talksWorld) leave(ch, user uuid.UUID) { w.mgr.apply(presenceEvent{user, ch, false}) }

func (w *talksWorld) exists(t *testing.T, id uuid.UUID) bool {
	t.Helper()
	var n int
	if err := w.p.QueryRow(context.Background(), `SELECT count(*) FROM channels WHERE id = $1`, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n == 1
}

func TestOpeningATalkCopiesTheLobbyAndNumbersFromOne(t *testing.T) {
	w := newTalksWorld(t)
	ctx := context.Background()
	if _, err := w.p.Exec(ctx, `INSERT INTO permission_overwrites (channel_id, user_id, allow, deny) VALUES ($1, $2, $3, $4)`,
		w.lobby.ID, w.bob.ID, int64(perm.Connect), int64(perm.Speak)); err != nil {
		t.Fatal(err)
	}
	first, err := w.mgr.Open(ctx, w.alice.ID, w.lobby.ID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := w.mgr.Open(ctx, w.bob.ID, w.lobby.ID)
	if err != nil {
		t.Fatal(err)
	}
	var name, name2 string
	var category *uuid.UUID
	var limit int
	var isTemp, isLobby bool
	var leader, from *uuid.UUID
	if err := w.p.QueryRow(ctx, `SELECT name, category_id, user_limit, is_temp, is_lobby, leader_id, lobby_id FROM channels WHERE id = $1`, first.ID).
		Scan(&name, &category, &limit, &isTemp, &isLobby, &leader, &from); err != nil {
		t.Fatal(err)
	}
	_ = w.p.QueryRow(ctx, `SELECT name FROM channels WHERE id = $1`, second.ID).Scan(&name2)
	if name != "Talk 1" || name2 != "Talk 2" {
		t.Fatalf("names = %q, %q; want Talk 1, Talk 2", name, name2)
	}
	if category == nil || *category != w.category || limit != 4 || !isTemp || isLobby {
		t.Fatalf("category %v limit %d temp %v lobby %v", category, limit, isTemp, isLobby)
	}
	if leader == nil || *leader != w.alice.ID || from == nil || *from != w.lobby.ID {
		t.Fatalf("leader %v lobby %v", leader, from)
	}
	var allow, deny int64
	if err := w.p.QueryRow(ctx, `SELECT allow, deny FROM permission_overwrites WHERE channel_id = $1 AND user_id = $2`, first.ID, w.bob.ID).Scan(&allow, &deny); err != nil {
		t.Fatalf("the lobby's overwrites are copied: %v", err)
	}
	if allow != int64(perm.Connect) || deny != int64(perm.Speak) {
		t.Fatalf("overwrite = %d/%d", allow, deny)
	}

	// The lowest free number is used again once a Talk is gone.
	if _, err := DeleteChannel(ctx, w.p, first.ID); err != nil {
		t.Fatal(err)
	}
	third, err := w.mgr.Open(ctx, w.alice.ID, w.lobby.ID)
	if err != nil {
		t.Fatal(err)
	}
	var name3 string
	_ = w.p.QueryRow(ctx, `SELECT name FROM channels WHERE id = $1`, third.ID).Scan(&name3)
	if name3 != "Talk 1" {
		t.Fatalf("third = %q; want the freed Talk 1", name3)
	}
	// The channel list marks the lobby and the temporary Talks.
	cats, _, err := GetServerHierarchy(ctx, w.p)
	if err != nil {
		t.Fatal(err)
	}
	var lobbies, temps int
	for _, ch := range cats[0].Channels {
		if ch.IsLobby {
			lobbies++
		}
		if ch.IsTemp && ch.LeaderID != nil {
			temps++
		}
	}
	if lobbies != 1 || temps != 2 {
		t.Fatalf("lobbies %d, temporary Talks %d", lobbies, temps)
	}
}

func TestOnlyALobbyOpensTalksAndOnlyUpToTheCap(t *testing.T) {
	w := newTalksWorld(t)
	ctx := context.Background()
	plain, err := CreateChannel(ctx, w.p, &w.category, "Plain", ChannelTypeVoice, "", 9)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.mgr.Open(ctx, w.alice.ID, plain.ID); !errors.Is(err, errNotLobby) {
		t.Fatalf("a plain Talk opens none: %v", err)
	}
	text, _ := CreateChannel(ctx, w.p, nil, "text", ChannelTypeText, "", 0)
	if err := SetLobby(ctx, w.p, text.ID, true); err == nil {
		t.Fatal("a text channel cannot be a lobby")
	}
	for i := 0; i < MaxTempTalksPerLobby; i++ {
		if _, err := OpenTempTalk(ctx, w.p, w.lobby.ID, w.alice.ID); err != nil {
			t.Fatalf("Talk %d: %v", i+1, err)
		}
	}
	if _, err := OpenTempTalk(ctx, w.p, w.lobby.ID, w.alice.ID); !errors.Is(err, errTooManyTalks) {
		t.Fatalf("over the cap: %v", err)
	}
}

func TestOpeningTalksIsRateLimitedPerMember(t *testing.T) {
	w := newTalksWorld(t)
	w.mgr.CreateLimit = httpx.NewRateLimiter(TalksPerWindow, TalkWindow)
	ctx := context.Background()
	for i := 0; i < TalksPerWindow; i++ {
		if _, err := w.mgr.Open(ctx, w.alice.ID, w.lobby.ID); err != nil {
			t.Fatalf("Talk %d: %v", i+1, err)
		}
	}
	var apiErr *httpx.APIError
	if _, err := w.mgr.Open(ctx, w.alice.ID, w.lobby.ID); !errors.As(err, &apiErr) || apiErr.Code != httpx.CodeRateLimited {
		t.Fatalf("sixth Talk: %v", err)
	}
	if _, err := w.mgr.Open(ctx, w.bob.ID, w.lobby.ID); err != nil {
		t.Fatalf("another member is not held back: %v", err)
	}
}

func TestLeadershipPassesToTheLongestMemberAndIsBroadcast(t *testing.T) {
	w := newTalksWorld(t)
	ctx := context.Background()
	carol := newTalksUser(t, w.p, "carol_t")
	talk, err := w.mgr.Open(ctx, w.alice.ID, w.lobby.ID)
	if err != nil {
		t.Fatal(err)
	}
	w.join(talk.ID, w.alice.ID)
	w.join(talk.ID, w.bob.ID)
	w.join(talk.ID, carol.ID)
	if got, _ := w.mgr.Leader(talk.ID); got != w.alice.ID {
		t.Fatalf("the opener leads: %v", got)
	}
	w.leave(talk.ID, w.bob.ID)
	if got, _ := w.mgr.Leader(talk.ID); got != w.alice.ID {
		t.Fatal("a member leaving does not change the leader")
	}
	w.leave(talk.ID, w.alice.ID)
	if got, _ := w.mgr.Leader(talk.ID); got != carol.ID {
		t.Fatalf("leader = %v; want carol, the member present longest", got)
	}
	w.join(talk.ID, w.bob.ID)
	w.leave(talk.ID, carol.ID)
	if got, _ := w.mgr.Leader(talk.ID); got != w.bob.ID {
		t.Fatalf("leader = %v; want bob", got)
	}
	var stored *uuid.UUID
	_ = w.p.QueryRow(ctx, `SELECT leader_id FROM channels WHERE id = $1`, talk.ID).Scan(&stored)
	if stored == nil || *stored != w.bob.ID {
		t.Fatalf("stored leader = %v", stored)
	}
	var changes int
	for _, ev := range w.rec.Snapshot() {
		if ev.Type == events.TalkLeader && ev.ChannelID == talk.ID {
			changes++
		}
	}
	if changes != 2 {
		t.Fatalf("talk_leader events = %d; want 2 (to carol, to bob)", changes)
	}
}

func TestAnEmptyTalkIsRemovedAfterTheGraceAndAuditedAsTheSystem(t *testing.T) {
	w := newTalksWorld(t)
	ctx := context.Background()
	kept, _ := w.mgr.Open(ctx, w.alice.ID, w.lobby.ID)
	gone, _ := w.mgr.Open(ctx, w.alice.ID, w.lobby.ID)
	w.join(kept.ID, w.alice.ID)
	w.join(gone.ID, w.bob.ID)
	w.leave(gone.ID, w.bob.ID)
	w.join(gone.ID, w.bob.ID) // back within the grace: stays
	w.leave(gone.ID, w.bob.ID)

	deadline := time.Now().Add(5 * time.Second)
	for w.exists(t, gone.ID) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if w.exists(t, gone.ID) {
		t.Fatal("the empty Talk is still there")
	}
	if !w.exists(t, kept.ID) {
		t.Fatal("an occupied Talk was removed")
	}
	var actor, name string
	var actorID *uuid.UUID
	if err := w.p.QueryRow(ctx, `SELECT actor_name, actor_id, target_name FROM audit_log WHERE action = 'channel.delete' AND target_id = $1`, gone.ID.String()).Scan(&actor, &actorID, &name); err != nil {
		t.Fatalf("the removal is audited: %v", err)
	}
	if actor != "system" || actorID != nil || name != "Talk 2" {
		t.Fatalf("audit entry: %q %v %q", actor, actorID, name)
	}
	if w.closed.Load() == 0 {
		t.Fatal("stragglers are disconnected from the removed room")
	}
}

func TestATalkNobodyEntersIsRemovedAndRestartsLeaveNoTalksBehind(t *testing.T) {
	w := newTalksWorld(t)
	ctx := context.Background()
	unused, _ := w.mgr.Open(ctx, w.alice.ID, w.lobby.ID)
	deadline := time.Now().Add(5 * time.Second)
	for w.exists(t, unused.ID) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if w.exists(t, unused.ID) {
		t.Fatal("a Talk nobody entered stays")
	}
	left, err := OpenTempTalk(ctx, w.p, w.lobby.ID, w.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	fresh := NewTalkManager(&TalkManager{DB: w.p, Events: w.rec, Perms: perm.NewService(w.p)})
	fresh.sweepStale(ctx)
	if w.exists(t, left.ID) {
		t.Fatal("a temporary Talk survived a restart")
	}
	if !w.exists(t, w.lobby.ID) {
		t.Fatal("the lobby itself must stay")
	}
}

func TestTemporaryTalksStayOutOfTheSavedLayout(t *testing.T) {
	w := newTalksWorld(t)
	ctx := context.Background()
	talk, err := OpenTempTalk(ctx, w.p, w.lobby.ID, w.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	err = ApplyLayout(ctx, w.p, nil, []ChannelPlacement{{ID: talk.ID, CategoryID: nil, SortOrder: 99}, {ID: w.lobby.ID, CategoryID: &w.category, SortOrder: 7}})
	if err != nil {
		t.Fatal(err)
	}
	var order int
	var category *uuid.UUID
	_ = w.p.QueryRow(ctx, `SELECT sort_order, category_id FROM channels WHERE id = $1`, talk.ID).Scan(&order, &category)
	if order == 99 || category == nil {
		t.Fatalf("a temporary Talk was placed by the layout: %d %v", order, category)
	}
}

func talkRequest(u *auth.User, body string, params map[string]string) *http.Request {
	return chatRequest(u, body, params, "")
}

func TestOnlyTheLeaderEditsATemporaryTalk(t *testing.T) {
	w := newTalksWorld(t)
	ctx := context.Background()
	h := &Handler{DB: w.p, Events: w.rec, Perms: perm.NewService(w.p), Talks: w.mgr}
	talk, _ := w.mgr.Open(ctx, w.alice.ID, w.lobby.ID)
	params := map[string]string{"id": talk.ID.String()}

	if err := h.updateTalk(httptest.NewRecorder(), talkRequest(w.bob, `{"name":"Mine"}`, params)); !errors.Is(err, errNotTalkLeader) {
		t.Fatalf("a member renames the Talk: %v", err)
	}
	rec := httptest.NewRecorder()
	if err := h.updateTalk(rec, talkRequest(w.alice, `{"name":"Raid night","user_limit":3}`, params)); err != nil {
		t.Fatal(err)
	}
	if body := rec.Body.String(); !strings.Contains(body, `"Raid night"`) || !strings.Contains(body, `"user_limit":3`) || !strings.Contains(body, `"is_temp":true`) {
		t.Fatalf("response = %s", body)
	}
	if err := h.updateTalk(httptest.NewRecorder(), talkRequest(w.alice, `{"user_limit":0}`, params)); err != nil {
		t.Fatalf("the leader clears the limit: %v", err)
	}
	// Not a temporary Talk: the leader route does not serve it.
	plain := map[string]string{"id": w.lobby.ID.String()}
	if err := h.updateTalk(httptest.NewRecorder(), talkRequest(w.alice, `{"name":"x"}`, plain)); !errors.Is(err, errNotTempTalk) {
		t.Fatalf("a permanent Talk: %v", err)
	}
	// Leadership moved on: the old leader loses the right.
	w.join(talk.ID, w.alice.ID)
	w.join(talk.ID, w.bob.ID)
	w.leave(talk.ID, w.alice.ID)
	if err := h.updateTalk(httptest.NewRecorder(), talkRequest(w.alice, `{"name":"again"}`, params)); !errors.Is(err, errNotTalkLeader) {
		t.Fatalf("the former leader: %v", err)
	}
	if err := h.updateTalk(httptest.NewRecorder(), talkRequest(w.bob, `{"name":"Bob's"}`, params)); err != nil {
		t.Fatalf("the new leader: %v", err)
	}
}

func TestStartingATalkFromAConversationPostsAnInvite(t *testing.T) {
	w := newTalksWorld(t)
	ctx := context.Background()
	h := &Handler{DB: w.p, Events: w.rec, Perms: perm.NewService(w.p), Talks: w.mgr}
	dm, _, err := CreateDM(ctx, w.p, NewDM{Creator: w.alice.ID, Others: []uuid.UUID{w.bob.ID}})
	if err != nil {
		t.Fatal(err)
	}
	params := map[string]string{"id": dm.String()}

	// Hide the only lobby from Alice: the community has none she can use.
	var everyone uuid.UUID
	_ = w.p.QueryRow(ctx, `SELECT id FROM roles WHERE is_everyone`).Scan(&everyone)
	if _, err := w.p.Exec(ctx, `INSERT INTO permission_overwrites (channel_id, role_id, deny) VALUES ($1, $2, $3)`, w.lobby.ID, everyone, int64(perm.ViewChannel)); err != nil {
		t.Fatal(err)
	}
	h.Perms.Invalidate()
	if err := h.startDMTalk(httptest.NewRecorder(), talkRequest(w.alice, ``, params)); !errors.Is(err, errNoTalkLobby) {
		t.Fatalf("no usable lobby: %v", err)
	}
	if _, err := w.p.Exec(ctx, `DELETE FROM permission_overwrites WHERE channel_id = $1`, w.lobby.ID); err != nil {
		t.Fatal(err)
	}
	h.Perms.Invalidate()

	rec := httptest.NewRecorder()
	if err := h.startDMTalk(rec, talkRequest(w.alice, ``, params)); err != nil {
		t.Fatal(err)
	}
	var talk uuid.UUID
	var embeds []byte
	if err := w.p.QueryRow(ctx, `SELECT c.id, m.embeds FROM messages m JOIN channels c ON c.id = (m.embeds->0->>'talk_id')::uuid WHERE m.channel_id = $1`, dm).Scan(&talk, &embeds); err != nil {
		t.Fatalf("the invitation card is in the conversation: %v", err)
	}
	if got, _ := w.mgr.Leader(talk); got != w.alice.ID {
		t.Fatalf("the starter leads: %v", got)
	}
	if !strings.Contains(string(embeds), "Talk 1") {
		t.Fatalf("card = %s", embeds)
	}
}

func TestAnInviteCardGrantsNothing(t *testing.T) {
	w := newTalksWorld(t)
	ctx := context.Background()
	h := &Handler{DB: w.p, Events: w.rec, Perms: perm.NewService(w.p), Talks: w.mgr}
	dm, _, err := CreateDM(ctx, w.p, NewDM{Creator: w.alice.ID, Others: []uuid.UUID{w.bob.ID}})
	if err != nil {
		t.Fatal(err)
	}
	// A Talk only Alice may see or join.
	private, err := CreateChannel(ctx, w.p, nil, "Leads", ChannelTypeVoice, "", 1)
	if err != nil {
		t.Fatal(err)
	}
	var everyone uuid.UUID
	_ = w.p.QueryRow(ctx, `SELECT id FROM roles WHERE is_everyone`).Scan(&everyone)
	if _, err := w.p.Exec(ctx, `INSERT INTO permission_overwrites (channel_id, role_id, deny) VALUES ($1, $2, $3)`, private.ID, everyone, int64(perm.ViewChannel|perm.Connect)); err != nil {
		t.Fatal(err)
	}
	if _, err := w.p.Exec(ctx, `INSERT INTO permission_overwrites (channel_id, user_id, allow) VALUES ($1, $2, $3)`, private.ID, w.alice.ID, int64(perm.ViewChannel|perm.Connect)); err != nil {
		t.Fatal(err)
	}
	h.Perms.Invalidate()
	body := `{"conversation_id":"` + dm.String() + `"}`
	params := map[string]string{"id": private.ID.String()}

	// Bob cannot see the Talk, so he cannot invite anyone to it.
	if err := h.inviteToTalk(httptest.NewRecorder(), talkRequest(w.bob, body, params)); err == nil {
		t.Fatal("a member who cannot join the Talk invited to it")
	}
	if err := h.inviteToTalk(httptest.NewRecorder(), talkRequest(w.alice, body, params)); err != nil {
		t.Fatalf("Alice may invite: %v", err)
	}
	// The card changed nothing about who may join: Bob still cannot.
	got, err := h.Perms.Channel(ctx, w.bob.Subject(), private.ID)
	if err == nil && got.Has(perm.ViewChannel|perm.Connect) {
		t.Fatal("the invitation let Bob into the Talk")
	}
	var overwrites int
	_ = w.p.QueryRow(ctx, `SELECT count(*) FROM permission_overwrites WHERE channel_id = $1`, private.ID).Scan(&overwrites)
	if overwrites != 2 {
		t.Fatalf("the invitation wrote permissions: %d overwrites", overwrites)
	}
	// Inviting into a conversation the caller is not in is refused.
	carol := newTalksUser(t, w.p, "carol_t")
	if err := h.inviteToTalk(httptest.NewRecorder(), talkRequest(carol, body, map[string]string{"id": w.lobby.ID.String()})); err == nil {
		t.Fatal("a stranger posted into a conversation")
	}
}
