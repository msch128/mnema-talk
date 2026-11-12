//go:build integration

package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
)

// callTalkAdminRoutes names every administrative route that takes a channel
// id, in an order that keeps the channel alive until the last one (DELETE).
func callTalkAdminRoutes(c *client, id uuid.UUID) map[string]response {
	p := "/api/admin/channels/" + id.String()
	out := map[string]response{}
	out["PATCH rename"] = c.patch(p, map[string]any{"name": "renamed-okapi"})
	out["PATCH lobby"] = c.patch(p, map[string]any{"is_lobby": true})
	out["PUT auto-delete"] = c.put(p+"/auto-delete", map[string]any{"days": 7})
	out["GET overwrites"] = c.get(p + "/overwrites")
	out["PUT overwrites"] = putOverwrites(c, "channels", id)
	out["GET webhooks"] = c.get(p + "/webhooks")
	out["POST webhooks"] = c.post(p+"/webhooks", map[string]any{"name": "hook"})
	out["GET outgoing-webhooks"] = c.get(p + "/outgoing-webhooks")
	out["POST outgoing-webhooks"] = c.post(p+"/outgoing-webhooks", map[string]any{"name": "o", "url": "https://example.com/h", "events": []string{"MESSAGE_CREATE"}})
	out["PUT layout"] = c.put("/api/admin/layout", map[string]any{"channels": []map[string]any{{"id": id, "category_id": nil, "sort_order": 3}}})
	out["POST duplicate"] = c.post(p+"/duplicate", nil)
	out["DELETE"] = c.delete(p)
	return out
}

// callTalkRouteOrder is the order the answers are compared in.
var callTalkRouteOrder = []string{"PATCH rename", "PATCH lobby", "PUT auto-delete", "GET overwrites", "PUT overwrites",
	"GET webhooks", "POST webhooks", "GET outgoing-webhooks", "POST outgoing-webhooks", "PUT layout", "POST duplicate", "DELETE"}

// answerOf is a response without the id of the path, so that two ids compare.
func answerOf(r response, id uuid.UUID) string {
	return http.StatusText(r.status) + " " + strings.ReplaceAll(string(r.body), id.String(), "<id>")
}

type callTalkAdminWorld struct {
	a          *app
	owner      *client
	adm, mgr   *client
	conv, call uuid.UUID
}

// newCallTalkAdminWorld: an Administrator and a member who holds only Manage
// channels talk to each other and start a call; the Owner is outside.
func newCallTalkAdminWorld(t *testing.T) callTalkAdminWorld {
	t.Helper()
	a := newApp(t, false)
	owner := a.seedAdmin()
	adm, mgr := a.register(owner, "admins"), a.register(owner, "manager")
	assignRoles(t, owner, adm, createRole(t, owner, "Admins", perm.Administrator))
	assignRoles(t, owner, mgr, createRole(t, owner, "Builders", perm.ManageChannels))
	conv := adm.mustDM(t, mgr).ID
	wantStatus(t, "the conversation works", adm.dmPost(conv, "hi"), http.StatusCreated)
	res := adm.post("/api/dms/"+conv.String()+"/talk", nil)
	wantStatus(t, "start a call", res, http.StatusCreated)
	var call struct {
		ID uuid.UUID `json:"channel_id"`
	}
	res.decode(t, &call)
	return callTalkAdminWorld{a: a, owner: owner, adm: adm, mgr: mgr, conv: conv, call: call.ID}
}

// differences lists the routes where the call answers other than a channel
// that does not exist, for the caller c.
func (w callTalkAdminWorld) differences(c *client) []string {
	ghost := uuid.New()
	var diff []string
	// The ghost first: nothing it does changes anything.
	want := callTalkAdminRoutes(c, ghost)
	got := callTalkAdminRoutes(c, w.call)
	for _, name := range callTalkRouteOrder {
		if answerOf(got[name], w.call) != answerOf(want[name], ghost) {
			diff = append(diff, name+": "+answerOf(got[name], w.call)+" instead of "+answerOf(want[name], ghost))
		}
	}
	return diff
}

// H-L6-06 (known failure). doc/talks.md: nothing about a conversation's call
// reaches the audit log, renaming it included, and doc/architecture.md: the
// administrative channel routes answer 404 for a conversation. For a call
// Talk they act: a member who holds Manage channels (or is an Administrator)
// and is in the conversation renames it (channel.update with the call's id and
// both names), duplicates it into a permanent community Talk (channel.duplicate)
// and so on, and the Owner and the Administrators outside the conversation read
// the entries.
//
// The assertion is inverted: the test passes while the call answers other than
// a missing channel and the audit log carries it, and turns red when it does not.
func TestKnownFailureAdminChannelRoutesActOnACallOfAConversation(t *testing.T) {
	w := newCallTalkAdminWorld(t)
	diff := w.differences(w.adm)
	if len(diff) == 0 {
		t.Fatal("H-L6-06 no longer reproduces: the admin routes answer a call like a missing channel. Flip this test into a guard")
	}
	log := w.owner.get("/api/admin/audit-log")
	wantStatus(t, "the Owner reads the audit log", log, http.StatusOK)
	if !strings.Contains(string(log.body), w.call.String()) {
		t.Fatal("the audit log no longer names the call. Flip this test into a guard")
	}
	t.Logf("H-L6-06 still reproduces: %d routes differ", len(diff))
	for _, d := range diff {
		t.Logf("  %.110s", d)
	}
}
