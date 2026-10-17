//go:build integration

package server

import (
	"net/http"
	"testing"

	"github.com/msch128/mnema-talk/internal/perm"
)

// Rotating the secret, renaming, switching on or off and changing the URL
// change what they say and nothing else; only a new URL is recorded (url_set_by),
// and who made the webhook stays.
func TestEditingAnOutgoingWebhookKeepsItsRecord(t *testing.T) {
	a := outgoingApp(t)
	admin := a.seedAdmin()
	ch := a.createChannel(admin, "alerts", "text")
	rc := newReceiver(t, nil)
	elsewhere := newReceiver(t, nil)
	role := createRole(t, admin, "admins", perm.Administrator)
	one, two := a.register(admin, "one"), a.register(admin, "two")
	assignRoles(t, admin, one, role)
	assignRoles(t, admin, two, role)

	o := makeOutgoing(t, one, ch, rc.srv.URL, "MESSAGE_CREATE")
	base := "/api/admin/outgoing-webhooks/" + o.ID.String()
	for _, req := range []response{
		two.post(base+"/secret", nil),
		two.patch(base, map[string]any{"name": "renamed"}),
		two.patch(base, map[string]any{"enabled": false}),
		two.patch(base, map[string]any{"enabled": true}),
		two.patch(base, map[string]any{"events": []string{"MESSAGE_CREATE", "MESSAGE_DELETE"}}),
		two.patch(base, map[string]any{"url": elsewhere.srv.URL}),
	} {
		if req.status != http.StatusOK {
			t.Fatalf("edit: %d %s", req.status, req.body)
		}
	}
	var by, urlBy string
	if err := a.db.QueryRow(t.Context(), `SELECT created_by::text, url_set_by::text FROM outgoing_webhooks WHERE id = $1`, o.ID).Scan(&by, &urlBy); err != nil || by != one.user.ID.String() || urlBy != two.user.ID.String() {
		t.Fatalf("created_by = %q, url_set_by = %q, %v", by, urlBy, err)
	}
	// It delivers to the new URL, whoever edited it.
	a.send(admin, ch, "to the new URL")
	elsewhere.next(t)
	rc.none(t)
}
