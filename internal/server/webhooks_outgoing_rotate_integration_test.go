//go:build integration

package server

import (
	"net/http"
	"testing"

	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/webhook"
)

// The holder of a rotated secret becomes the creator, and deliveries follow
// their rights from then on.
func TestRotatingASecretTransfersTheOutgoingWebhook(t *testing.T) {
	a := outgoingApp(t)
	admin := a.seedAdmin()
	ch := a.createChannel(admin, "alerts", "text")
	rc := newReceiver(t, nil)
	role := createRole(t, admin, "hooks", perm.ManageWebhooks)
	one, two := a.register(admin, "one"), a.register(admin, "two")
	assignRoles(t, admin, one, role)
	assignRoles(t, admin, two, role)

	o := makeOutgoing(t, one, ch, rc.srv.URL, "MESSAGE_CREATE")
	var rot webhook.CreatedOutgoing
	res := two.post("/api/admin/outgoing-webhooks/"+o.ID.String()+"/secret", nil)
	res.decode(t, &rot)
	if res.status != http.StatusOK || rot.CreatedBy == nil || *rot.CreatedBy != two.user.ID {
		t.Fatalf("rotate: %d %s", res.status, res.body)
	}
	// The first creator losing the role changes nothing now ...
	assignRoles(t, admin, one)
	a.send(admin, ch, "still delivered")
	rc.next(t)
	// ... but the new holder losing it stops the deliveries.
	assignRoles(t, admin, two)
	a.send(admin, ch, "not delivered")
	rc.none(t)
}
