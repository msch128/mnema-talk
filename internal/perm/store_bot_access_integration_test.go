//go:build integration

package perm_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
)

// applyAccess runs ApplyBotAccess in a transaction of its own and commits only
// when it succeeds, like the install does.
func applyAccess(t *testing.T, w *world, a perm.BotAccess) error {
	t.Helper()
	ctx := context.Background()
	tx, err := w.p.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := perm.ApplyBotAccess(ctx, tx, a); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func ownRolePermissions(t *testing.T, w *world, role uuid.UUID) (p perm.Set, markedAll bool) {
	t.Helper()
	ctx := context.Background()
	if err := w.p.QueryRow(ctx, `SELECT permissions FROM roles WHERE id = $1`, role).Scan(&p); err != nil {
		t.Fatal(err)
	}
	if err := w.p.QueryRow(ctx, `SELECT access_all FROM bots WHERE user_id = (SELECT bot_user_id FROM roles WHERE id = $1)`, role).Scan(&markedAll); err != nil {
		t.Fatal(err)
	}
	return p, markedAll
}

func categoryOverwriteOf(t *testing.T, w *world, role uuid.UUID) (allow int64, found bool) {
	t.Helper()
	err := w.p.QueryRow(context.Background(), `SELECT allow FROM permission_overwrites WHERE category_id = $1 AND role_id = $2`, w.category, role).Scan(&allow)
	return allow, err == nil
}

func TestApplyingABotsAccessRefusesWhatIsNotItsToGive(t *testing.T) {
	w := newWorld(t)
	role := botWithOwnRole(t, w)
	_, talk := lobbyWithTalk(t, w)

	wantAPI(t, "an elevated permission", applyAccess(t, w, perm.BotAccess{Role: role, All: true, Allow: perm.Administrator | perm.ViewChannel}), http.StatusBadRequest)
	wantAPI(t, "an admin-area permission", applyAccess(t, w, perm.BotAccess{Role: role, Allow: perm.ManageChannels, Channels: []uuid.UUID{w.channel}}), http.StatusBadRequest)
	if err := applyAccess(t, w, perm.BotAccess{Role: w.everyone, All: true, Allow: perm.ViewChannel}); err == nil || apiCode(err) != 0 {
		t.Fatalf("@everyone is no bot's own role: want an internal error, got %v", err)
	}
	wantAPI(t, "a temporary Talk", applyAccess(t, w, perm.BotAccess{Role: role, Allow: perm.ViewChannel, Channels: []uuid.UUID{talk}}), http.StatusConflict)
	if p, marked := ownRolePermissions(t, w, role); p != 0 || marked {
		t.Fatalf("a refusal changed the role: %b %v", p, marked)
	}
}

func TestApplyingABotsAccessSwitchesBetweenAllAndSelectedChannels(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	role := botWithOwnRole(t, w)
	var other uuid.UUID
	if err := w.p.QueryRow(ctx, `INSERT INTO channels (name, type) VALUES ('other', 'text') RETURNING id`).Scan(&other); err != nil {
		t.Fatal(err)
	}
	const work = perm.ViewChannel | perm.SendMessages

	if err := applyAccess(t, w, perm.BotAccess{Role: role, All: true, Allow: work, Channels: []uuid.UUID{w.channel}}); err != nil {
		t.Fatal(err)
	}
	if p, marked := ownRolePermissions(t, w, role); p != work || !marked {
		t.Fatalf("all: %b %v", p, marked)
	}
	if allow, _, found := overwriteOf(t, w, w.channel, role); !found || perm.Set(allow) != work {
		t.Fatalf("listed channel: %b %v", allow, found)
	}

	// Selected channels: the server-wide grants go, the listed channel's overwrite is rewritten.
	if err := applyAccess(t, w, perm.BotAccess{Role: role, Allow: perm.ViewChannel, Channels: []uuid.UUID{other}}); err != nil {
		t.Fatal(err)
	}
	if p, marked := ownRolePermissions(t, w, role); p != 0 || marked {
		t.Fatalf("selected: %b %v", p, marked)
	}
	if _, _, found := overwriteOf(t, w, w.channel, role); found {
		t.Fatal("the overwrite of a channel no longer chosen stayed")
	}
	if allow, _, found := overwriteOf(t, w, other, role); !found || perm.Set(allow) != perm.ViewChannel {
		t.Fatalf("chosen channel: %b %v", allow, found)
	}

	// Nothing to allow: no overwrite is written and the old ones go.
	if err := applyAccess(t, w, perm.BotAccess{Role: role, Channels: []uuid.UUID{other}}); err != nil {
		t.Fatal(err)
	}
	if _, _, found := overwriteOf(t, w, other, role); found {
		t.Fatal("an overwrite stayed although nothing is allowed")
	}
}

func TestApplyingABotsAccessKeepsWhatTheCallerMayNotTouch(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	role := botWithOwnRole(t, w)
	var hidden uuid.UUID
	if err := w.p.QueryRow(ctx, `INSERT INTO channels (name, type) VALUES ('hidden', 'text') RETURNING id`).Scan(&hidden); err != nil {
		t.Fatal(err)
	}
	for _, ch := range []uuid.UUID{w.channel, hidden} {
		if _, err := w.p.Exec(ctx, `INSERT INTO permission_overwrites (channel_id, role_id, allow, deny) VALUES ($1, $2, $3, 0)`, ch, role, int64(perm.SendMessages)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := w.p.Exec(ctx, `INSERT INTO permission_overwrites (category_id, role_id, allow, deny) VALUES ($1, $2, $3, 0)`, w.category, role, int64(perm.ViewChannel)); err != nil {
		t.Fatal(err)
	}

	// Restricted to w.channel and no category: the other channel and the category stay.
	if err := applyAccess(t, w, perm.BotAccess{Role: role, Restrict: true, Within: []uuid.UUID{w.channel}}); err != nil {
		t.Fatal(err)
	}
	if _, _, found := overwriteOf(t, w, w.channel, role); found {
		t.Fatal("the overwrite in a channel within reach stayed")
	}
	if _, _, found := overwriteOf(t, w, hidden, role); !found {
		t.Fatal("an overwrite outside reach was removed")
	}
	if _, found := categoryOverwriteOf(t, w, role); !found {
		t.Fatal("a category outside reach was cleared")
	}

	// Within reach of the category and everything, they go.
	if err := applyAccess(t, w, perm.BotAccess{Role: role, Restrict: true, Within: []uuid.UUID{hidden}, WithinCategories: []uuid.UUID{w.category}}); err != nil {
		t.Fatal(err)
	}
	if _, _, found := overwriteOf(t, w, hidden, role); found {
		t.Fatal("the overwrite within reach stayed")
	}
	if _, found := categoryOverwriteOf(t, w, role); found {
		t.Fatal("the category within reach stayed")
	}
}

// Each statement of the change reports its failure: every one of them is
// broken in turn inside a transaction that is then rolled back.
func TestApplyingABotsAccessReportsEveryFailingStatement(t *testing.T) {
	for _, tc := range []struct{ name, sql string }{
		{"load the role", `ALTER TABLE roles RENAME COLUMN bot_user_id TO bot_user_id_x`},
		{"set the role's permissions", `ALTER TABLE roles RENAME COLUMN updated_at TO updated_at_x`},
		{"mark the bot", `ALTER TABLE bots RENAME COLUMN access_all TO access_all_x`},
		{"replace the channel overwrites", `ALTER TABLE permission_overwrites RENAME COLUMN allow TO allow_x`},
		{"replace the category overwrites", `ALTER TABLE permission_overwrites RENAME COLUMN category_id TO category_id_x`},
		{"remove the empty overwrites", `ALTER TABLE permission_overwrites RENAME COLUMN deny TO deny_x`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			role := botWithOwnRole(t, w)
			ctx := context.Background()
			tx, err := w.p.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(ctx) }()
			if _, err := tx.Exec(ctx, tc.sql); err != nil {
				t.Fatal(err)
			}
			a := perm.BotAccess{Role: role, All: true, Allow: perm.ViewChannel, Channels: []uuid.UUID{w.channel}}
			if err := perm.ApplyBotAccess(ctx, tx, a); err == nil {
				t.Fatal("a broken statement went unnoticed")
			}
		})
	}
}

func TestChangingABotsAccessThroughTheService(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	role := botWithOwnRole(t, w)
	var botUser uuid.UUID
	if err := w.p.QueryRow(ctx, `SELECT bot_user_id FROM roles WHERE id = $1`, role).Scan(&botUser); err != nil {
		t.Fatal(err)
	}
	access := perm.BotAccess{All: true, Allow: perm.ViewChannel | perm.SendMessages}

	if err := w.svc.SetBotAccess(ctx, w.owner, botUser, access); err != nil {
		t.Fatal(err)
	}
	if p, marked := ownRolePermissions(t, w, role); p != access.Allow || !marked {
		t.Fatalf("after the change: %b %v", p, marked)
	}
	snap, err := w.svc.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !snap.HasAllChannelAccess(botUser) {
		t.Fatal("the snapshot does not know the choice")
	}
	wantAPI(t, "a member without Manage bots", w.svc.SetBotAccess(ctx, w.member, botUser, access), http.StatusForbidden)
	hidden := access
	hidden.Channels = []uuid.UUID{uuid.New()}
	wantAPI(t, "a channel that does not exist", w.svc.SetBotAccess(ctx, w.owner, botUser, hidden), http.StatusNotFound)

	// A bot without an own role (deleted since the request began).
	var roleless uuid.UUID
	if err := w.p.QueryRow(ctx, `INSERT INTO users (username, display_name, password_hash, role, is_bot) VALUES ('bot-n', 'n', '!', 'user', TRUE) RETURNING id`).Scan(&roleless); err != nil {
		t.Fatal(err)
	}
	if _, err := w.p.Exec(ctx, `INSERT INTO bots (user_id, name) VALUES ($1, 'n')`, roleless); err != nil {
		t.Fatal(err)
	}
	wantAPI(t, "a bot without its role", w.svc.SetBotAccess(ctx, w.owner, roleless, access), http.StatusNotFound)
}
