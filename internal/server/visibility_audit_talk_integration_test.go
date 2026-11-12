//go:build integration

package server

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/msch128/mnema-talk/internal/moderation"
	"github.com/msch128/mnema-talk/internal/perm"
)

// H-L6-03. doc/moderation.md, "Access": who was in which Talk is read only by
// holders of View audit log who can view the Talk (the voice.* entries). A
// moderator removing a member from one private Talk says the same: the entry
// reaches only auditors who can view that Talk, and the Owner. The admin-wide
// kick names no Talk and stays readable to every auditor. Rows written before
// the per-Talk removal had its own action (user.talk_kick with the Talk in
// details) are classed by that Talk when they are read.
func TestTalkRemovalFromAPrivateTalkReachesOnlyAuditorsWhoSeeIt(t *testing.T) {
	f := newAccessFixture(t, true)
	talk := f.privateChannel(t, "staff-talk", "voice")
	lobby := f.a.createChannel(f.owner, "lobby", "voice")
	auditors := createRole(t, f.owner, "Auditors", perm.ViewAuditLog)
	auditor := f.a.register(f.owner, "auditor")
	assignRoles(t, f.owner, auditor, auditors)
	assignRoles(t, f.owner, f.insider, f.staff, auditors)
	inTalk := f.a.register(f.owner, "okapitalk")
	assignRoles(t, f.owner, inTalk, f.staff)
	wide := f.a.register(f.owner, "okapiwide")
	old := f.a.register(f.owner, "okapiold")

	watcher := f.owner.dialLiveWS(f.a.origin())
	joinTalk(t, f.a, watcher, inTalk, talk)
	wantStatus(t, "per-Talk removal", f.owner.post(disconnectURL(talk, inTalk), nil), http.StatusNoContent)
	joinTalk(t, f.a, watcher, wide, lobby)
	wantStatus(t, "admin-wide kick", f.owner.post("/api/admin/users/"+wide.user.ID.String()+"/kick", nil), http.StatusNoContent)
	// What the per-Talk removal wrote before it had an action of its own.
	if _, err := f.a.db.Exec(context.Background(), `INSERT INTO audit_log (actor_id, actor_name, action, target_type, target_id, target_name, details)
		VALUES ($1, 'owner', 'user.talk_kick', 'user', $2, 'okapiold', jsonb_build_object('channel_id', $3::text))`,
		f.owner.user.ID, old.user.ID.String(), talk.String()); err != nil {
		t.Fatal(err)
	}

	if visibleChannelIDs(t, auditor)[talk] {
		t.Fatal("the auditor can see the Talk")
	}
	for who, c := range map[string]*client{"auditor": auditor, "staff auditor": f.insider, "owner": f.owner} {
		seesTalk := who != "auditor"
		if got := strings.Contains(string(c.get("/api/admin/audit-log?limit=200").body), talk.String()); got != seesTalk {
			t.Errorf("%s reads the Talk's id: %v, want %v", who, got, seesTalk)
		}
		removed := talkRemovals(t, c)
		for target, want := range map[*client]bool{inTalk: seesTalk, old: seesTalk, wide: true} {
			if got := slices.Contains(removed, target.user.ID.String()); got != want {
				t.Errorf("%s reads the Talk removal of %s: %v, want %v", who, target.user.Username, got, want)
			}
		}
	}
	// The moderation history still lists the per-Talk removal, without the Talk.
	var hist moderation.History
	res := f.owner.get("/api/admin/users/" + inTalk.user.ID.String() + "/moderation")
	wantStatus(t, "history", res, http.StatusOK)
	res.decode(t, &hist)
	if !slices.ContainsFunc(hist.Events, func(e moderation.HistoryEvent) bool { return e.Kind == "talk_removal" }) {
		t.Errorf("the moderation history lacks the Talk removal: %+v", hist.Events)
	}
	if strings.Contains(string(res.body), talk.String()) {
		t.Error("the moderation history names the Talk")
	}
}

// doc/talks.md: nothing about a conversation's call Talk reaches the audit
// log, "because that would tell the Owner who called whom". A member of the
// conversation who may disconnect members removes the other one from the
// call: the Owner, an Administrator and an auditor (none of them in the
// conversation) read nothing about it, and the moderation history, which
// would say the member was in some call, does not list it either.
func TestARemovalFromACallTalkIsNotAudited(t *testing.T) {
	f := newCallFixture(t)
	owner, alice, bob := f.dmFixture.admin, f.dmFixture.alice, f.dmFixture.bob
	assignRoles(t, owner, alice, createRole(t, owner, "Mods", perm.DisconnectMembers))
	administrator := f.a.register(owner, "administrator")
	assignRoles(t, owner, administrator, createRole(t, owner, "Admins", perm.Administrator))
	auditor := f.a.register(owner, "auditor")
	assignRoles(t, owner, auditor, createRole(t, owner, "Auditors", perm.ViewAuditLog))
	talk := f.start(t)

	joinTalk(t, f.a, alice.dialLiveWS(f.a.origin()), bob, talk)
	wantStatus(t, "removal from the call", alice.post(disconnectURL(talk, bob), nil), http.StatusNoContent)

	for who, c := range map[string]*client{"owner": owner, "administrator": administrator, "auditor": auditor} {
		if strings.Contains(string(c.get("/api/admin/audit-log?limit=200").body), talk.String()) {
			t.Errorf("%s reads the call Talk's id", who)
		}
		if slices.Contains(talkRemovals(t, c), bob.user.ID.String()) {
			t.Errorf("%s reads the removal from the call", who)
		}
	}
	var hist moderation.History
	res := owner.get("/api/admin/users/" + bob.user.ID.String() + "/moderation")
	wantStatus(t, "history", res, http.StatusOK)
	res.decode(t, &hist)
	if slices.ContainsFunc(hist.Events, func(e moderation.HistoryEvent) bool { return e.Kind == "talk_removal" }) {
		t.Errorf("the moderation history lists the removal from the call: %+v", hist.Events)
	}
}

// talkRemovals lists the targets of the Talk removals the reader sees.
func talkRemovals(t *testing.T, c *client) []string {
	t.Helper()
	var out []string
	for _, e := range auditEntries(t, c, "target_type=user&limit=200").Entries {
		if e.Action == "user.talk_kick" || e.Action == "voice.disconnect" {
			out = append(out, e.TargetID)
		}
	}
	return out
}
