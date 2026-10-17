//go:build integration

package server

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
)

func visibleTargets(t *testing.T, c *client, query string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, e := range auditEntries(t, c, query+"&limit=200").Entries {
		out[e.Action+"|"+e.TargetID] = true
	}
	return out
}

// A holder of View audit log reads message and Talk entries only for
// channels they can view right now, and never sign-in events.
func TestAuditScopesChannelEntriesAndHidesSignIns(t *testing.T) {
	a := newApp(t, true)
	admin := a.seedAdmin()
	open := a.createChannel(admin, "open", "text")
	secret := a.createChannel(admin, "secret", "text")
	secretTalk := a.createChannel(admin, "secret-talk", "voice")
	openTalk := a.createChannel(admin, "open-talk", "voice")

	everyone := everyoneID(t, admin)
	for _, ch := range []uuid.UUID{secret, secretTalk} {
		if res := putOverwrites(admin, "channels", ch, overwrite("role", everyone, 0, perm.ViewChannel)); res.status != http.StatusNoContent {
			t.Fatalf("hide: %d %s", res.status, res.body)
		}
	}

	auditorRole := createRole(t, admin, "auditor", perm.ViewAuditLog)
	auditor := a.register(admin, "audra")
	assignRoles(t, admin, auditor, auditorRole)

	openMsg := a.send(admin, open, "public words")
	secretMsg := a.send(admin, secret, "private words")
	for _, id := range []struct{ ch, msg uuid.UUID }{{open, openMsg}, {secret, secretMsg}} {
		if res := admin.delete("/api/channels/" + id.ch.String() + "/messages/" + id.msg.String()); res.status != http.StatusNoContent {
			t.Fatal(res.status)
		}
	}
	conn, _, err := admin.dialWS(a.origin())
	if err != nil {
		t.Fatal(err)
	}
	for _, talk := range []uuid.UUID{openTalk, secretTalk} {
		conn.send("voice_join", map[string]any{"channel_id": talk})
		conn.send("voice_leave", nil)
	}
	waitForAudit(t, admin, "action=voice", 4)

	got := visibleTargets(t, auditor, "")
	if !got["message.delete|"+openMsg.String()] || !got["voice.join|"+openTalk.String()] {
		t.Errorf("auditor misses entries of viewable channels: %v", got)
	}
	for key := range got {
		for _, hidden := range []string{secretMsg.String(), secretTalk.String()} {
			if len(key) >= len(hidden) && key[len(key)-len(hidden):] == hidden {
				t.Errorf("auditor sees %s of a channel they cannot view", key)
			}
		}
		if len(key) > 5 && key[:5] == "auth." {
			t.Errorf("auditor sees %s", key)
		}
	}
	if len(visibleTargets(t, auditor, "action=auth")) != 0 {
		t.Error("auditor reads sign-in events")
	}
	if res := auditor.get("/api/admin/audit-log?target_id=" + secretMsg.String()); res.status != http.StatusOK {
		t.Fatal(res.status)
	} else if len(auditEntries(t, auditor, "target_id="+secretMsg.String()).Entries) != 0 {
		t.Error("a target_id filter reaches a hidden channel's entry")
	}

	// The Owner sees all, and so does an auditor who gains the channel.
	all := visibleTargets(t, admin, "")
	if !all["message.delete|"+secretMsg.String()] || !all["voice.join|"+secretTalk.String()] || len(visibleTargets(t, admin, "action=auth")) == 0 {
		t.Errorf("owner misses entries: %v", all)
	}
	if res := putOverwrites(admin, "channels", secret, overwrite("role", auditorRole, perm.ViewChannel, 0)); res.status != http.StatusNoContent {
		t.Fatalf("grant: %d %s", res.status, res.body)
	}
	if !visibleTargets(t, auditor, "")["message.delete|"+secretMsg.String()] {
		t.Error("access is decided when reading: the auditor can view the channel now")
	}
}
