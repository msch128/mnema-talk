//go:build integration

package push

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/perm"
)

func TestANewMessageReachesExactlyTheMembersWhoShouldBeTold(t *testing.T) {
	e := newEnv(t, http.StatusCreated)
	alice, bob, carol := e.member("alice"), e.member("bob"), e.member("carol")
	dave, erin, frank := e.member("dave"), e.member("erin"), e.member("frank")
	gina, hank, ida := e.member("gina"), e.member("hank"), e.member("ida")

	// carol: mentions only. dave: muted. erin: do not disturb. frank: account
	// disabled. gina: category muted, channel set back to all. hank: nothing set.
	e.exec(`INSERT INTO channel_reads (user_id, channel_id, notify_level) VALUES ($1, $3, 'mentions'), ($2, $3, 'mute'), ($4, $3, 'all')`, carol.id, dave.id, e.general, gina.id)
	e.exec(`UPDATE users SET presence = 'dnd' WHERE id = $1`, erin.id)
	e.exec(`UPDATE users SET disabled_at = NOW() WHERE id = $1`, frank.id)
	e.exec(`INSERT INTO category_notify (user_id, category_id, notify_level) VALUES ($1, $2, 'mute')`, gina.id, e.category)
	e.exec(`UPDATE users SET push_preview = FALSE WHERE id = $1`, ida.id)

	e.publish(message(alice, e.general, "Hello **team**", carol))

	for _, m := range []member{bob, carol, gina, hank, ida} {
		if got := e.pushedTo("/device/"+m.name, 1); len(got) != 1 {
			t.Fatalf("%s: %d pushes", m.name, len(got))
		}
	}
	settle()
	for _, m := range []member{alice, dave, erin, frank} {
		if got := e.requestsTo("/device/" + m.name); len(got) != 0 {
			t.Errorf("%s was told although they should not be (%d pushes)", m.name, len(got))
		}
	}

	bobs := e.content(bob, e.requestsTo("/device/bob")[0])
	if bobs.Title != "Display alice (#general)" || bobs.Body != "Hello team" || bobs.Mention || bobs.Tag != e.general.String() {
		t.Fatalf("bob's push: %+v", bobs)
	}
	if idas := e.content(ida, e.requestsTo("/device/ida")[0]); idas.Body != "New message" || strings.Contains(idas.Title+idas.Body, "Hello") {
		t.Fatalf("ida chose no preview: %+v", idas)
	}
	carols := e.requestsTo("/device/carol")[0]
	if p := e.content(carol, carols); !p.Mention || carols.header.Get("Urgency") != "high" || carols.header.Get("Ttl") != "86400" {
		t.Fatalf("carol was mentioned: %+v urgency %q ttl %q", p, carols.header.Get("Urgency"), carols.header.Get("Ttl"))
	}
	if h := e.requestsTo("/device/bob")[0].header; h.Get("Urgency") != "normal" || h.Get("Ttl") != "3600" || h.Get("Topic") == "" {
		t.Fatalf("bob's headers: %v", h)
	}
}

func TestAMemberWhoCannotViewTheChannelIsNeverTold(t *testing.T) {
	e := newEnv(t, http.StatusCreated)
	alice, bob, owner := e.member("alice"), e.member("bob"), e.member("owner")
	e.perms.Owner(owner.id)

	e.publish(message(alice, e.secret, "private words", bob, owner))
	e.pushedTo("/device/owner", 1)
	settle()
	if got := e.requestsTo("/device/bob"); len(got) != 0 {
		t.Fatalf("bob cannot view #secret but got %d pushes", len(got))
	}
}

func TestAnEndedMuteNotifiesAgain(t *testing.T) {
	e := newEnv(t, http.StatusCreated)
	alice, bob := e.member("alice"), e.member("bob")
	e.exec(`INSERT INTO channel_reads (user_id, channel_id, notify_level, muted_until) VALUES ($1, $2, 'mute', NOW() + INTERVAL '1 hour')`, bob.id, e.general)
	e.publish(message(alice, e.general, "first"))
	settle()
	if got := e.requestsTo("/device/bob"); len(got) != 0 {
		t.Fatal("pushed during a mute")
	}
	e.exec(`UPDATE channel_reads SET muted_until = NOW() - INTERVAL '1 minute' WHERE user_id = $1`, bob.id)
	e.publish(message(alice, e.general, "second"))
	e.pushedTo("/device/bob", 1)
}

func TestTheOperatorCanForbidMessageText(t *testing.T) {
	e := newEnv(t, http.StatusCreated)
	e.svc.Previews = false
	alice, bob := e.member("alice"), e.member("bob")
	e.publish(message(alice, e.general, "top secret plans"))
	p := e.content(bob, e.pushedTo("/device/bob", 1)[0])
	if p.Body != "New message" || strings.Contains(p.Title+p.Body, "secret") {
		t.Fatalf("%+v", p)
	}
}

func TestABurstInOneChannelIsOneGroupedPush(t *testing.T) {
	e := newEnvWith(t, http.StatusCreated, 250*time.Millisecond)
	alice, bob := e.member("alice"), e.member("bob")
	for _, text := range []string{"one", "two", "three", "four"} {
		e.publish(message(alice, e.general, text))
	}
	got := e.pushedTo("/device/bob", 2)
	settle()
	if n := len(e.requestsTo("/device/bob")); n != 2 {
		t.Fatalf("%d pushes for four messages, want 1 + 1 grouped", n)
	}
	first, second := e.content(bob, got[0]), e.content(bob, got[1])
	if first.Body != "one" {
		t.Fatalf("first push %+v", first)
	}
	if second.Title != "#general" || second.Body != "3 new messages\nDisplay alice: four" {
		t.Fatalf("grouped push %+v", second)
	}
}

func TestEveryDeviceOfAMemberIsTold(t *testing.T) {
	e := newEnv(t, http.StatusCreated)
	alice, bob := e.member("alice"), e.member("bob")
	e.subscribe(bob, "/device/bob-phone")
	e.publish(message(alice, e.general, "hi"))
	e.pushedTo("/device/bob", 1)
	e.pushedTo("/device/bob-phone", 1)
}

// switchablePerms lets a test change what the permission service answers while
// the delivery service runs.
type switchablePerms struct{ cur atomic.Pointer[perm.Service] }

func (s *switchablePerms) Snapshot(ctx context.Context) (*perm.Snapshot, error) {
	return s.cur.Load().Snapshot(ctx)
}

func TestAHeldPushIsDroppedWhenTheMemberLosesTheChannelInsideTheWindow(t *testing.T) {
	cases := map[string]func(e *env, sw *switchablePerms, bob member){
		"access revoked": func(e *env, sw *switchablePerms, bob member) {
			sw.cur.Store(e.perms.Fork().MemberOverwrite(true, e.general, bob.id, 0, perm.ViewChannel).Service())
		},
		"channel muted": func(e *env, _ *switchablePerms, bob member) {
			e.exec(`INSERT INTO channel_reads (user_id, channel_id, notify_level) VALUES ($1, $2, 'mute')`, bob.id, e.general)
		},
		"author blocked": func(e *env, _ *switchablePerms, bob member) {
			e.exec(`INSERT INTO user_blocks (blocker_id, blocked_id) SELECT $1, id FROM users WHERE username = 'alice'`, bob.id)
		},
		"notification level set to mentions": func(e *env, _ *switchablePerms, bob member) {
			e.exec(`INSERT INTO channel_reads (user_id, channel_id, notify_level) VALUES ($1, $2, 'mentions')`, bob.id, e.general)
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnvWith(t, http.StatusCreated, time.Second)
			sw := &switchablePerms{}
			sw.cur.Store(e.perms.Service())
			e.svc.Perms = sw // set before the first message is published
			alice, bob := e.member("alice"), e.member("bob")
			e.publish(message(alice, e.general, "first"))
			e.pushedTo("/device/bob", 1)
			e.publish(message(alice, e.general, "held back"))
			time.Sleep(50 * time.Millisecond) // let the second message reach the window
			change(e, sw, bob)
			time.Sleep(1500 * time.Millisecond) // the window ends
			if n := len(e.requestsTo("/device/bob")); n != 1 {
				t.Fatalf("%d pushes, want only the one sent before the change", n)
			}
		})
	}
}

func TestAMemberWhoBlockedTheAuthorIsNeverToldWhateverTheMessage(t *testing.T) {
	e := newEnv(t, http.StatusCreated)
	alice, bob, carol := e.member("alice"), e.member("bob"), e.member("carol")
	e.exec(`INSERT INTO user_blocks (blocker_id, blocked_id) VALUES ($1, $2)`, bob.id, alice.id)

	e.publish(message(alice, e.general, "plain"))
	e.publish(message(alice, e.general, "hey bob", bob, carol))
	e.pushedTo("/device/carol", 1)
	settle()
	if got := e.requestsTo("/device/bob"); len(got) != 0 {
		t.Fatalf("bob blocked alice but got %d pushes", len(got))
	}
	// The block is one way: alice is still told about bob's messages.
	e.publish(message(bob, e.general, "hello alice", alice))
	e.pushedTo("/device/alice", 1)
}

func TestADirectMessageIsPushedToItsOtherMembersWithTheSenderAsTitle(t *testing.T) {
	e := newEnv(t, http.StatusCreated)
	alice, bob, carol := e.member("alice"), e.member("bob"), e.member("carol")
	dm := e.perms.DM(alice.id, bob.id)
	e.exec(`INSERT INTO channels (id, number, name, type) OVERRIDING SYSTEM VALUE VALUES ($1, -1, '', 'dm')`, dm)

	e.publish(message(alice, dm, "psst, only for you", carol))
	got := e.pushedTo("/device/bob", 1)
	settle()
	p := e.content(bob, got[0])
	if p.Title != "Display alice" || p.Body != "psst, only for you" || strings.Contains(p.Title, "#") || p.ChannelID == nil || *p.ChannelID != dm {
		t.Fatalf("bob's push: %+v", p)
	}
	if got[0].header.Get("Urgency") != "high" {
		t.Errorf("a direct message is urgent: %v", got[0].header)
	}
	for _, m := range []member{alice, carol} {
		if n := len(e.requestsTo("/device/" + m.name)); n != 0 {
			t.Errorf("%s was pushed %d times although not in the conversation", m.name, n)
		}
	}
	// A muted conversation stays silent, although it is addressed to the member.
	e.exec(`INSERT INTO channel_reads (user_id, channel_id, notify_level) VALUES ($1, $2, 'mute')`, bob.id, dm)
	e.publish(message(alice, dm, "again", bob))
	settle()
	if n := len(e.requestsTo("/device/bob")); n != 1 {
		t.Errorf("bob muted the conversation but has %d pushes", n)
	}
}

func TestAHeldPushStillGoesOutWhenNothingChanged(t *testing.T) {
	e := newEnvWith(t, http.StatusCreated, 200*time.Millisecond)
	alice := e.member("alice")
	e.member("bob")
	e.publish(message(alice, e.general, "first"))
	e.publish(message(alice, e.general, "second"))
	e.pushedTo("/device/bob", 2)
}

func TestOnlyMessageCreationsArePushed(t *testing.T) {
	e := newEnv(t, http.StatusCreated)
	alice := e.member("alice")
	e.member("bob")
	msg := message(alice, e.general, "edited")
	e.svc.Observe(e.general, events.MessageUpdate, msg)
	e.svc.Observe(e.general, events.MessageDelete, map[string]any{"id": msg.ID})
	e.svc.Observe(e.general, events.MessageCreate, "not a message")
	e.svc.Observe(e.general, events.MessageCreate, &msg) // a pointer works like a value
	e.pushedTo("/device/bob", 1)
	settle()
	if n := len(e.requestsTo("/device/bob")); n != 1 {
		t.Fatalf("%d pushes, want 1", n)
	}
}

func TestASubscriptionTheBrowserEndedIsRemoved(t *testing.T) {
	e := newEnv(t, http.StatusGone)
	alice, bob := e.member("alice"), e.member("bob")
	e.publish(message(alice, e.general, "hi"))
	e.pushedTo("/device/bob", 1)
	waitFor(t, func() bool { return countSubscriptions(e, bob.id) == 0 }, "the 410 subscription to be removed")
}

func TestASubscriptionThatKeepsFailingIsRemoved(t *testing.T) {
	e := newEnv(t, http.StatusForbidden)
	alice, bob := e.member("alice"), e.member("bob")
	for i := 0; i < maxFailures; i++ {
		if countSubscriptions(e, bob.id) != 1 {
			t.Fatalf("removed after %d failures", i)
		}
		e.publish(message(alice, e.general, "again"))
		want := i + 1
		waitFor(t, func() bool { return len(e.requestsTo("/device/bob")) >= want }, "the next push attempt")
		waitFor(t, func() bool { return failuresOf(e, bob.id) >= want || countSubscriptions(e, bob.id) == 0 }, "the failure to be counted")
	}
	waitFor(t, func() bool { return countSubscriptions(e, bob.id) == 0 }, "the failing subscription to be removed")
}

func TestATemporaryRefusalIsRetriedOnce(t *testing.T) {
	e := newEnv(t, http.StatusServiceUnavailable)
	alice := e.member("alice")
	e.member("bob")
	e.publish(message(alice, e.general, "hi"))
	e.pushedTo("/device/bob", 2)
	settle()
	if n := len(e.requestsTo("/device/bob")); n != 2 {
		t.Fatalf("%d attempts, want 2", n)
	}
}

func TestADeliveredPushResetsTheFailureCountAndNotesTheTime(t *testing.T) {
	e := newEnv(t, http.StatusCreated)
	alice, bob := e.member("alice"), e.member("bob")
	e.exec(`UPDATE push_subscriptions SET failures = 4 WHERE user_id = $1`, bob.id)
	e.publish(message(alice, e.general, "hi"))
	waitFor(t, func() bool { return failuresOf(e, bob.id) == 0 && lastSuccessSet(e, bob.id) }, "the delivery to be recorded")
}

func TestTheTestButtonReachesEveryDeviceAndReportsTheResult(t *testing.T) {
	e := newEnv(t, http.StatusCreated)
	bob := e.member("bob")
	e.subscribe(bob, "/device/bob-phone")
	res, err := e.svc.SendTest(context.Background(), bob.id)
	if err != nil || res.Delivered != 2 || res.Failed != 0 {
		t.Fatalf("%+v, %v", res, err)
	}
	p := e.content(bob, e.requestsTo("/device/bob")[0])
	if p.Type != "test" || p.Body != "Push works on this device." {
		t.Fatalf("%+v", p)
	}

	e.push.status.Store(http.StatusGone)
	res, err = e.svc.SendTest(context.Background(), bob.id)
	if err != nil || res.Delivered != 0 || res.Failed != 2 || countSubscriptions(e, bob.id) != 0 {
		t.Fatalf("%+v, %v, %d devices left", res, err, countSubscriptions(e, bob.id))
	}
}

func TestADeletedAccountTakesItsDevicesAlong(t *testing.T) {
	e := newEnv(t, http.StatusCreated)
	bob := e.member("bob")
	e.exec(`DELETE FROM users WHERE id = $1`, bob.id)
	if n := countSubscriptions(e, bob.id); n != 0 {
		t.Fatalf("%d subscriptions left", n)
	}
}

func countSubscriptions(e *env, user uuid.UUID) int {
	var n int
	if err := e.pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM push_subscriptions WHERE user_id = $1`, user).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func failuresOf(e *env, user uuid.UUID) int {
	var n int
	_ = e.pool.QueryRow(context.Background(), `SELECT COALESCE(MAX(failures), 0) FROM push_subscriptions WHERE user_id = $1`, user).Scan(&n)
	return n
}

func lastSuccessSet(e *env, user uuid.UUID) bool {
	var set bool
	_ = e.pool.QueryRow(context.Background(), `SELECT COALESCE(bool_and(last_success_at IS NOT NULL), FALSE) FROM push_subscriptions WHERE user_id = $1`, user).Scan(&set)
	return set
}

func waitFor(t *testing.T, ok func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// A device lives and dies with the session that registered it.
func TestADeviceStopsReceivingWhenItsSessionEnds(t *testing.T) {
	cases := map[string]string{
		"signed out":                             `INSERT INTO revoked_sessions (jti, expires_at) VALUES ('sess-1', NOW() + INTERVAL '1 day')`,
		"password changed or log out everywhere": `UPDATE users SET token_version = token_version + 1 WHERE username = 'bob'`,
		"session expired":                        `UPDATE push_subscriptions SET session_expires_at = NOW() - INTERVAL '1 minute' WHERE user_id = (SELECT id FROM users WHERE username = 'bob')`,
	}
	for name, ending := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, http.StatusCreated)
			alice, bob := e.member("alice"), e.member("bob")
			e.exec(`UPDATE push_subscriptions SET session_id = 'sess-1', session_expires_at = NOW() + INTERVAL '1 day' WHERE user_id = $1`, bob.id)
			e.publish(message(alice, e.general, "still valid"))
			e.pushedTo("/device/bob", 1)

			e.exec(ending)
			e.publish(message(alice, e.general, "after the end"))
			settle()
			if n := len(e.requestsTo("/device/bob")); n != 1 {
				t.Fatalf("%d pushes, want only the one before the session ended", n)
			}
			if devices, _ := list(context.Background(), e.pool, bob.id); len(devices) != 0 {
				t.Fatalf("the ended session's device is still listed: %+v", devices)
			}
			n, err := pruneEnded(context.Background(), e.pool)
			if err != nil || n != 1 || countSubscriptions(e, bob.id) != 0 {
				t.Fatalf("pruned %d (%v), %d left", n, err, countSubscriptions(e, bob.id))
			}
		})
	}
}

func TestRegisteringRecordsTheSessionAndOlderDevicesOfEndedSessionsDrop(t *testing.T) {
	e := newEnv(t, http.StatusCreated)
	bob := e.member("bob")
	e.exec(`UPDATE users SET token_version = 3 WHERE id = $1`, bob.id)
	exp := time.Now().Add(time.Hour)
	sub, err := upsert(context.Background(), e.pool, bob.id, newSubscription{
		endpoint: e.push.URL + "/second", p256dh: bob.browser.p256dh(), auth: bob.browser.secret(), sessionID: "sess-2", sessionExpires: &exp,
	})
	if err != nil {
		t.Fatal(err)
	}
	var tv int
	var sid string
	if err := e.pool.QueryRow(context.Background(), `SELECT token_version, session_id FROM push_subscriptions WHERE id = $1`, sub.ID).Scan(&tv, &sid); err != nil || tv != 3 || sid != "sess-2" {
		t.Fatalf("recorded token_version %d session %q (%v)", tv, sid, err)
	}
	// The first device was registered before the version moved on: its session ended with it.
	if devices, _ := list(context.Background(), e.pool, bob.id); len(devices) != 1 || devices[0].ID != sub.ID {
		t.Fatalf("%+v", devices)
	}
}

// stored writes the message and who it mentions how (0 by name, 1 through @all or @here, 2 through a role),
// like the chat package does before the message reaches the hub.
func (e *env) stored(msg chat.Message, kind int, who ...member) chat.Message {
	e.t.Helper()
	e.exec(`INSERT INTO messages (id, channel_id, user_id, content) VALUES ($1, $2, $3, $4)`, msg.ID, msg.ChannelID, msg.UserID, msg.Content)
	for _, m := range who {
		e.exec(`INSERT INTO message_mentions (message_id, user_id, kind) VALUES ($1, $2, $3)`, msg.ID, m.id, kind)
		msg.Mentions = append(msg.Mentions, m.id)
	}
	return msg
}

func TestAMuteLetsThroughWhatIsAddressedToTheMemberButNotAtAll(t *testing.T) {
	e := newEnv(t, http.StatusCreated)
	alice, bob := e.member("alice"), e.member("bob")
	e.exec(`INSERT INTO channel_reads (user_id, channel_id, notify_level) VALUES ($1, $2, 'mute')`, bob.id, e.general)

	e.publish(message(alice, e.general, "just chatting"))
	e.publish(e.stored(message(alice, e.general, "@all stand-up"), 1, bob))
	settle()
	if got := e.requestsTo("/device/bob"); len(got) != 0 {
		t.Fatalf("a muted channel pushed %d times for an ordinary message and @all", len(got))
	}

	e.publish(e.stored(message(alice, e.general, "@bob are you there?"), 0, bob))
	p := e.content(bob, e.pushedTo("/device/bob", 1)[0])
	if !p.Mention || p.Body != "@bob are you there?" {
		t.Fatalf("push for a mention in a muted channel: %+v", p)
	}
}

func TestADoNotDisturbStillSilencesAMentionInAMutedChannel(t *testing.T) {
	e := newEnv(t, http.StatusCreated)
	alice, bob := e.member("alice"), e.member("bob")
	e.exec(`INSERT INTO channel_reads (user_id, channel_id, notify_level) VALUES ($1, $2, 'mute')`, bob.id, e.general)
	e.exec(`UPDATE users SET presence = 'dnd' WHERE id = $1`, bob.id)
	e.publish(e.stored(message(alice, e.general, "@bob"), 0, bob))
	settle()
	if got := e.requestsTo("/device/bob"); len(got) != 0 {
		t.Fatalf("%d pushes during do not disturb", len(got))
	}
}
