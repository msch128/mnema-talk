//go:build integration

package push

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestADirectNoticeReachesEveryDeviceOfTheMemberOnly(t *testing.T) {
	e := newEnv(t, http.StatusCreated)
	alice, bob := e.member("alice"), e.member("bob")
	e.subscribe(bob, "/device/bob-phone")
	ch, msg := e.general, uuid.New()

	e.svc.Direct(context.Background(), Direct{
		User: bob.id, Kind: DirectReminder, ID: uuid.New(), ChannelID: &ch, MessageID: &msg, Sender: "Alice", Content: "bring the cake",
	})
	first := e.pushedTo("/device/bob", 1)[0]
	e.pushedTo("/device/bob-phone", 1)
	p := e.content(bob, first)
	if p.Title != "Reminder" || p.Body != "Alice: bring the cake" || p.MessageID == nil || *p.MessageID != msg {
		t.Fatalf("push: %+v", p)
	}
	if first.header.Get("Urgency") != "high" {
		t.Fatalf("a reminder is urgent: %v", first.header)
	}
	settle()
	if got := e.requestsTo("/device/" + alice.name); len(got) != 0 {
		t.Fatalf("another member got the notice: %d", len(got))
	}
}

func TestADirectNoticeKeepsTheTextBackWhenPreviewsAreOff(t *testing.T) {
	e := newEnv(t, http.StatusCreated)
	e.svc.Previews = false
	bob := e.member("bob")
	e.svc.Direct(context.Background(), Direct{User: bob.id, Kind: DirectReminder, ID: uuid.New(), Sender: "Alice", Content: "top secret plans"})
	p := e.content(bob, e.pushedTo("/device/bob", 1)[0])
	if strings.Contains(p.Title+p.Body, "secret") || strings.Contains(p.Body, "Alice") {
		t.Fatalf("%+v", p)
	}
}

func TestADirectNoticeToAMemberWithoutDevicesOrWhoseAccountIsOffSendsNothing(t *testing.T) {
	e := newEnv(t, http.StatusCreated)
	bob := e.member("bob")
	e.exec(`DELETE FROM push_subscriptions WHERE user_id = $1`, bob.id)
	e.svc.Direct(context.Background(), Direct{User: bob.id, Kind: DirectScheduledFailed, ID: uuid.New()})
	e.svc.Direct(context.Background(), Direct{User: uuid.New(), Kind: DirectReminder, ID: uuid.New(), Gone: true})
	settle()
	if got := e.requestsTo("/device/bob"); len(got) != 0 {
		t.Fatalf("%d pushes without a device", len(got))
	}
}
