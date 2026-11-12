//go:build integration

package server

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// The guard of the erased_person_guard migration (H-L10-05): every foreign key to users runs
// erased_person_guard, which waits for an erasure in progress and then
// refuses, drops or clears a row that names the erased account. A new foreign
// key to users fails here until its migration adds the trigger.

// guardModes are the columns whose late row is not refused: rows of other
// members about the person, which the erasure deletes (drop) or clears
// (clear). Every other column refuses (the person's own rows and acts).
var guardModes = map[string]string{
	"message_mentions.user_id":         "drop",  // another member's message mentions them
	"thread_follows.user_id":           "drop",  // a reply follows the thread for its root's author
	"user_blocks.blocked_id":           "drop",  // another member blocks them
	"dm_members.user_id":               "drop",  // another member adds them to a conversation
	"user_roles.user_id":               "drop",  // a moderator assigns them a role
	"permission_overwrites.user_id":    "drop",  // a moderator sets channel access for them
	"message_reports.reported_user_id": "clear", // another member reports their message
	"channels.leader_id":               "clear", // the talk lobby hands the lead on
}

// guardSkips are foreign keys to users without the guard.
var guardSkips = map[string]string{
	"bots.user_id":         "a bot's own account, never a person (erasureKeeps)",
	"webhooks.bot_user_id": "the account a webhook posts as, never a person (erasureKeeps)",
}

// guardedColumns maps table.column to the mode its erased_person_guard
// trigger gives it, for triggers that fire before insert and before an update
// of that column.
func (a *app) guardedColumns() map[string]string {
	a.t.Helper()
	out := map[string]string{}
	rows, err := a.db.Query(context.Background(), `
		SELECT c.relname, tg.tgtype, encode(tg.tgargs, 'escape'),
		       ARRAY(SELECT attname FROM pg_attribute WHERE attrelid = tg.tgrelid AND attnum = ANY (tg.tgattr))::text[]
		FROM pg_trigger tg JOIN pg_class c ON c.oid = tg.tgrelid
		WHERE tg.tgname = 'erased_person_guard' AND NOT tg.tgisinternal`)
	if err != nil {
		a.t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var table, args string
		var kind int16
		var updateOf []string
		if err := rows.Scan(&table, &kind, &args, &updateOf); err != nil {
			a.t.Fatal(err)
		}
		const row, before, insert, update = 1, 2, 4, 16
		if kind&(row|before|insert|update) != row|before|insert|update {
			a.t.Errorf("%s: erased_person_guard must fire before insert and update of each row (tgtype %d)", table, kind)
		}
		for _, spec := range strings.Split(strings.TrimSuffix(args, `\000`), `\000`) {
			col, mode, _ := strings.Cut(spec, ":")
			if !slices.Contains(updateOf, col) {
				a.t.Errorf("%s.%s: the trigger does not fire on an update of the column", table, col)
			}
			out[table+"."+col] = mode
		}
	}
	return out
}

func TestEveryForeignKeyToUsersRefusesAnErasedAccount(t *testing.T) {
	a := newApp(t, false)
	guarded := a.guardedColumns()
	rows, err := a.db.Query(context.Background(), `
		SELECT k.conrelid::regclass::text || '.' || att.attname FROM pg_constraint k
		JOIN pg_attribute att ON att.attrelid = k.conrelid AND att.attnum = ANY (k.conkey)
		WHERE k.contype = 'f' AND k.confrelid = 'users'::regclass ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	fks, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, c := range fks {
		seen[c] = true
		mode, ok := guarded[c]
		_, skipped := guardSkips[c]
		want := guardModes[c]
		if want == "" {
			want = "refuse"
		}
		switch {
		case skipped && ok:
			t.Errorf("%s is guarded and listed in guardSkips: drop the entry", c)
		case skipped:
		case !ok:
			t.Errorf("%s: no erased_person_guard trigger names the column; add `CREATE OR REPLACE TRIGGER erased_person_guard BEFORE INSERT OR UPDATE OF <every person column of the table> ON <table> FOR EACH ROW EXECUTE FUNCTION erased_person_guard('<column>:<mode>', ...)` to its migration, naming the table's existing guarded columns too (the trigger replaces the old one)", c)
		case mode != want:
			t.Errorf("%s: mode %q, want %q (guardModes)", c, mode, want)
		}
	}
	for _, list := range []map[string]string{guardModes, guardSkips} {
		for c := range list {
			if !seen[c] {
				t.Errorf("the list names %s, which is no foreign key to users", c)
			}
		}
	}
}

// The rows other members write about an erased account still go through: a
// reply in the thread of a deleted member (its follow for the root's author is
// dropped) and a report of their emptied message (its reported account is
// cleared).
func TestOtherMembersStillWriteAboutAnErasedAccount(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	alice, bob := a.register(admin, "alice"), a.register(admin, "bob")
	ch := a.createChannel(admin, "general", "text")
	root := a.send(alice, ch, "a thread of alice's")
	if res := a.deleteAccount(alice, memberPassword, ""); res.status != http.StatusNoContent {
		t.Fatalf("delete: %d %s", res.status, res.body)
	}
	reply := bob.post("/api/channels/"+ch.String()+"/messages", map[string]any{"content": "a reply", "parent_id": root})
	wantStatus(t, "reply", reply, http.StatusCreated)
	if n := a.countRows(`SELECT COUNT(*) FROM thread_follows WHERE user_id = $1`, alice.user.ID); n != 0 {
		t.Errorf("the reply made the erased account follow the thread (%d rows)", n)
	}
	if n := a.countRows(`SELECT COUNT(*) FROM thread_follows WHERE user_id = $1 AND thread_id = $2`, bob.user.ID, root); n != 1 {
		t.Errorf("the replier follows the thread %d times, want 1", n)
	}
	report := bob.post("/api/messages/"+root.String()+"/report", map[string]any{"category": "spam"})
	wantStatus(t, "report", report, http.StatusCreated)
	if n := a.countRows(`SELECT COUNT(*) FROM message_reports WHERE message_id = $1 AND reported_user_id IS NULL`, root); n != 1 {
		t.Errorf("the report of the erased account's message: %d rows without the account, want 1", n)
	}
}

// A report written before the erasure keeps the erased person's words in its
// copy of the conversation (as the reported message or as a neighbour); the
// erasure's scrub of existing copies blanks both, and names the tombstone.
func TestTheErasureScrubsTheReportCopiesThatHoldThePerson(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	alice, bob := a.register(admin, "alice"), a.register(admin, "bob")
	ch := a.createChannel(admin, "general", "text")
	hers := a.send(alice, ch, "aliceswordsx7q")
	admins := a.send(admin, ch, "the admin's words")
	wantStatus(t, "report of hers", bob.post("/api/messages/"+hers.String()+"/report", map[string]any{"category": "spam"}), http.StatusCreated)
	wantStatus(t, "report next to hers", bob.post("/api/messages/"+admins.String()+"/report", map[string]any{"category": "spam"}), http.StatusCreated)
	if res := a.deleteAccount(alice, memberPassword, ""); res.status != http.StatusNoContent {
		t.Fatalf("delete: %d %s", res.status, res.body)
	}
	if hits := a.everyRowMentioning("aliceswordsx7q"); len(hits) > 0 {
		t.Errorf("the erased person's words stay in %v", hits)
	}
	if n := a.countRows(`SELECT COUNT(*) FROM message_reports WHERE snapshot::text LIKE '%"username": "alice"%'`); n != 0 {
		t.Errorf("%d report copies still name alice", n)
	}
	if n := a.countRows(`SELECT COUNT(*) FROM message_reports WHERE snapshot::text LIKE '%Deleted member%'`); n != 2 {
		t.Errorf("%d report copies show the tombstone, want 2", n)
	}
}
