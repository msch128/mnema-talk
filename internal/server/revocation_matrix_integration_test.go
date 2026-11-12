//go:build integration

package server

import (
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/slash"
)

// The revocation matrix: after a right is taken away, every live thing that
// depended on it stops within a stated time. Each row takes one right away
// from the victim of a fresh revocationWorld; each column probes one surface
// and measures how long it stayed open. A cell either stops within its bound
// or is listed in the row's open set with the reason it stays by design.
//
// Bounds (cited in each cell's verdict):
//   - events: at once. BroadcastChannel filters every event on a snapshot the
//     change invalidated before it answered (internal/ws/perms.go:53-79,
//     internal/perm/service.go:208-221).
//   - socket: at once for a sign-out (internal/ws/lifecycle.go:100, "The
//     server closes the connection when the session is revoked"); ban and kick
//     keep it banDisconnectDelay (300 ms) to flush the notice.
//   - seat: at once (internal/ws/perms.go:156-160, "removed from it at once");
//     after a disconnect the listing lingers for VoiceGrace while the media
//     peer is removed at once (internal/ws/voice_presence.go:12-28).
//   - saved, mentions, following, attachment: re-checked on every read.
//   - scheduled: checked when it is sent (internal/later/scheduled_send.go).

const (
	cellEvents     = "events"
	cellSocket     = "socket"
	cellSeat       = "seat"
	cellSaved      = "saved"
	cellMentions   = "mentions"
	cellFollowing  = "following"
	cellAttachment = "attachment"
	cellScheduled  = "scheduled"
)

var revocationCells = []string{cellEvents, cellSocket, cellSeat, cellSaved, cellMentions, cellFollowing, cellAttachment, cellScheduled}

// testGrace shortens the grace period so a lingering listing is measured to
// its end; the default is 30 s (internal/ws/hub.go:154).
const testGrace = time.Second

type revocationRow struct {
	name    string
	prepare func(*revocationWorld)
	revoke  func(*testing.T, *revocationWorld)
	// open lists the cells that keep working by design, with the reason.
	open map[string]string
}

// cellResult is one measured cell.
type cellResult struct {
	stopped bool
	took    time.Duration
	note    string
}

func (r cellResult) String() string {
	state := "OPEN"
	if r.stopped {
		state = "stops"
	}
	return fmt.Sprintf("%s %v %s", state, r.took.Round(time.Millisecond), r.note)
}

const sessionEnds = "the account keeps its rights; only the session ended"

// socketIsNoRight is why a permission change leaves the socket open.
const socketIsNoRight = "a socket is no right: every event is filtered on its own (internal/ws/perms.go:53-79)"

// flushWindow is why a ban or kick still delivers for banDisconnectDelay.
const flushWindow = "the socket stays banDisconnectDelay (300 ms, internal/auth/memberadmin/moderation.go:29) to flush the notice; EndingUser stops what it sends, not what it receives (internal/ws/lifecycle_pumps.go:37)"

func onlyCells(reason string, cells ...string) map[string]string {
	out := map[string]string{}
	for _, c := range cells {
		out[c] = reason
	}
	return out
}

func revocationRows() []revocationRow {
	signedOut := map[string]string{cellScheduled: sessionEnds}
	perms := map[string]string{cellSocket: socketIsNoRight}
	return []revocationRow{
		{name: "voice disconnect", revoke: adminPost("/kick", nil),
			open: onlyCells("POST /api/admin/users/{id}/kick (Disconnect members) only ends the Talk seat (internal/auth/memberadmin/users_admin.go:23)",
				cellEvents, cellSocket, cellSaved, cellMentions, cellFollowing, cellAttachment, cellScheduled)},
		{name: "kick", revoke: slashKick,
			open: map[string]string{cellEvents: flushWindow, cellScheduled: "a kick is no sanction, the member may sign in again (internal/auth/memberadmin/moderation_kick.go:32-37)"}},
		{name: "ban", revoke: adminPost("/ban", map[string]any{"reason": ""}), open: map[string]string{cellEvents: flushWindow}},
		{name: "timeout", revoke: adminPost("/timeout", map[string]any{"duration_seconds": 600}),
			open: map[string]string{
				cellEvents: "a timeout silences, it does not hide (doc/glossary.md)", cellSocket: "same", cellSaved: "same",
				cellMentions: "same", cellFollowing: "same", cellAttachment: "same",
			}},
		{name: "role removed", revoke: func(t *testing.T, w *revocationWorld) { assignRoles(t, w.owner, w.victim) }, open: perms},
		{name: "role permission changed", prepare: adminRoleInsteadOfStaff, revoke: func(t *testing.T, w *revocationWorld) {
			wantStatus(t, "drop admin", w.owner.patch("/api/admin/roles/"+w.bossRole(t).String(), map[string]any{"permissions": 0}), http.StatusOK)
		}, open: perms},
		{name: "channel overwrite changed", revoke: func(t *testing.T, w *revocationWorld) {
			for _, ch := range []uuid.UUID{w.text, w.talk} {
				wantStatus(t, "hide", putOverwrites(w.owner, "channels", ch, overwrite("role", w.everyone, 0, perm.ViewChannel)), http.StatusNoContent)
			}
		}, open: perms},
		{name: "channel made private", prepare: publicChannels, revoke: func(t *testing.T, w *revocationWorld) {
			for _, ch := range []uuid.UUID{w.text, w.talk} {
				wantStatus(t, "make private", putOverwrites(w.owner, "channels", ch, overwrite("role", w.everyone, 0, perm.ViewChannel)), http.StatusNoContent)
			}
		}, open: perms},
		{name: "channel deleted", revoke: func(t *testing.T, w *revocationWorld) {
			for _, ch := range []uuid.UUID{w.text, w.talk} {
				wantStatus(t, "delete", w.owner.delete("/api/admin/channels/"+ch.String()), http.StatusNoContent)
			}
		}, open: perms},
		{name: "account disabled", revoke: adminPost("/disable", nil)},
		{name: "account deleted", revoke: func(t *testing.T, w *revocationWorld) {
			wantStatus(t, "delete account", w.a.deleteAccount(w.victim, memberPassword, ""), http.StatusNoContent)
		}},
		{name: "sessions revoked by an admin", revoke: adminPost("/sessions/revoke", nil), open: signedOut},
		{name: "signed out", revoke: func(t *testing.T, w *revocationWorld) {
			wantStatus(t, "logout", w.victim.post("/api/auth/logout", nil), http.StatusNoContent)
		}, open: signedOut},
		{name: "password changed", revoke: func(t *testing.T, w *revocationWorld) {
			res := w.victim.put("/api/auth/password", map[string]string{"current_password": memberPassword, "new_password": "another-password-456"})
			wantStatus(t, "change password", res, http.StatusNoContent)
		}, open: onlyCells("the session that changed the password stays signed in; the others and every socket end (internal/auth/authn/handler_profile.go:100-135)",
			cellSaved, cellMentions, cellFollowing, cellAttachment, cellScheduled)},
		{name: "second factor reset", prepare: func(w *revocationWorld) { w.a.enroll(w.victim, memberPassword) },
			revoke: adminPost("/two-factor/reset", map[string]string{"current_password": "admin-password-123"}), open: signedOut},
	}
}

// adminPost is a row whose revocation is an Owner's POST on the victim.
func adminPost(suffix string, body any) func(*testing.T, *revocationWorld) {
	return func(t *testing.T, w *revocationWorld) {
		res := w.owner.post("/api/admin/users/"+w.victim.user.ID.String()+suffix, body)
		if res.status != http.StatusNoContent && res.status != http.StatusOK {
			t.Fatalf("%s: %d %s", suffix, res.status, res.body)
		}
	}
}

// cellBound is the time a stopping cell may take, from the revocation's
// answer. Only the seat and the socket are timed against a bound; the other
// probes run one after the other and their time is when they were asked.
func cellBound(row, cell string) time.Duration {
	switch cell {
	case cellSeat:
		if strings.Contains("sessions revoked by an admin|signed out|password changed|second factor reset", row) {
			return testGrace + 500*time.Millisecond // the listing lingers for the grace period
		}
		return 500 * time.Millisecond
	case cellSocket:
		return time.Second
	}
	return time.Hour
}

// slashKick is the community kick (/kick): sessions end, sockets close.
func slashKick(t *testing.T, w *revocationWorld) {
	res := w.owner.post("/api/channels/"+w.text.String()+"/interactions",
		map[string]any{"command_id": slash.ID("kick"), "options": map[string]any{"member": w.victim.user.ID}})
	wantStatus(t, "/kick", res, http.StatusOK)
}

// adminRoleInsteadOfStaff gives the victim Administrator (which passes every
// overwrite) instead of the Staff role.
func adminRoleInsteadOfStaff(w *revocationWorld) {
	boss := createRole(w.a.t, w.owner, "Boss", perm.Administrator)
	assignRoles(w.a.t, w.owner, w.victim, boss)
}

func (w *revocationWorld) bossRole(t *testing.T) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := w.a.db.QueryRow(t.Context(), `SELECT id FROM roles WHERE name = 'Boss'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// publicChannels lets @everyone into both channels and takes Staff away, so
// the victim's access rests on @everyone alone.
func publicChannels(w *revocationWorld) {
	for _, ch := range []uuid.UUID{w.text, w.talk} {
		wantStatus(w.a.t, "open", putOverwrites(w.owner, "channels", ch), http.StatusNoContent)
	}
	assignRoles(w.a.t, w.owner, w.victim)
}

func TestRevocationMatrix(t *testing.T) {
	var table []string
	for _, row := range revocationRows() {
		t.Run(strings.ReplaceAll(row.name, " ", "_"), func(t *testing.T) {
			w := newRevocationWorld(t, row.prepare)
			row.revoke(t, w)
			got := w.measure(t)
			line := []string{row.name}
			for _, cell := range revocationCells {
				res := got[cell]
				line = append(line, cell+": "+res.String())
				reason, byDesign := row.open[cell]
				switch {
				case !res.stopped && !byDesign:
					t.Errorf("%s / %s stayed open: %s", row.name, cell, res)
				case res.stopped && res.took > cellBound(row.name, cell):
					t.Errorf("%s / %s stopped after %v, bound %v", row.name, cell, res.took, cellBound(row.name, cell))
				case res.stopped && byDesign && res.note != naNoSession:
					t.Logf("%s / %s stopped although listed as open by design (%s)", row.name, cell, reason)
				}
			}
			table = append(table, strings.Join(line, " | "))
		})
	}
	sort.Strings(table)
	t.Log("\n" + strings.Join(table, "\n"))
	if out := os.Getenv("REVOCATION_MATRIX_OUT"); out != "" {
		if err := os.WriteFile(out, []byte(strings.Join(table, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
