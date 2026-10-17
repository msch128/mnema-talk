//go:build integration

package server

import (
	"net/http"
	"testing"

	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/webhook"
)

// The URL decides who may see the events, so only changing the URL moves the
// webhook to another creator; the secret, the name and the switch do not.
func TestOnlyChangingTheURLTransfersAnOutgoingWebhook(t *testing.T) {
	a := outgoingApp(t)
	admin := a.seedAdmin()
	ch := a.createChannel(admin, "alerts", "text")
	rc := newReceiver(t, nil)
	elsewhere := newReceiver(t, nil)
	role := createRole(t, admin, "hooks", perm.ManageWebhooks)
	one, two := a.register(admin, "one"), a.register(admin, "two")
	assignRoles(t, admin, one, role)
	assignRoles(t, admin, two, role)
	creatorOf := func(id string) string {
		t.Helper()
		var by string
		if err := a.db.QueryRow(t.Context(), `SELECT created_by::text FROM outgoing_webhooks WHERE id = $1`, id).Scan(&by); err != nil {
			t.Fatal(err)
		}
		return by
	}

	o := makeOutgoing(t, one, ch, rc.srv.URL, "MESSAGE_CREATE")
	base := "/api/admin/outgoing-webhooks/" + o.ID.String()

	// Rotating, renaming and switching off and on keep the creator.
	var rot webhook.CreatedOutgoing
	res := two.post(base+"/secret", nil)
	res.decode(t, &rot)
	if res.status != http.StatusOK || rot.CreatedBy == nil || *rot.CreatedBy != one.user.ID {
		t.Fatalf("rotate: %d %s", res.status, res.body)
	}
	for _, body := range []map[string]any{{"name": "renamed"}, {"enabled": false}, {"enabled": true}, {"events": []string{"MESSAGE_CREATE", "MESSAGE_DELETE"}}} {
		if r := two.patch(base, body); r.status != http.StatusOK {
			t.Fatalf("patch %v: %d %s", body, r.status, r.body)
		}
		if creatorOf(o.ID.String()) != one.user.ID.String() {
			t.Fatalf("patch %v moved the webhook", body)
		}
	}
	// So the first creator's rights still decide: losing them stops it.
	assignRoles(t, admin, one)
	a.send(admin, ch, "not delivered")
	rc.none(t)
	assignRoles(t, admin, one, role)
	a.send(admin, ch, "delivered again")
	rc.next(t)

	// Pointing it somewhere else makes the editor the creator.
	if r := two.patch(base, map[string]any{"url": elsewhere.srv.URL}); r.status != http.StatusOK {
		t.Fatalf("change URL: %d %s", r.status, r.body)
	}
	if creatorOf(o.ID.String()) != two.user.ID.String() {
		t.Fatal("the editor of the URL did not become the creator")
	}
	assignRoles(t, admin, one) // the first creator no longer matters
	a.send(admin, ch, "to the new URL")
	elsewhere.next(t)
	assignRoles(t, admin, two)
	a.send(admin, ch, "not delivered to the new URL")
	elsewhere.none(t)
}
