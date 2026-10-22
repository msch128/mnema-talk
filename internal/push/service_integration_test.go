//go:build integration

package push

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/events"
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
	if bobs.Title != "Display alice (#general)" || bobs.Body != "Hello **team**" || bobs.Mention || bobs.Tag != e.general.String() {
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
