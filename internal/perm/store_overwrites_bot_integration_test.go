//go:build integration

package perm_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
)

// A bot with its own managed role, as bot.Store creates it.
func botWithOwnRole(t *testing.T, w *world) (role uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	var botUser uuid.UUID
	if err := w.p.QueryRow(ctx, `INSERT INTO users (username, display_name, password_hash, role, is_bot) VALUES ('bot-m', 'm', '!', 'user', TRUE) RETURNING id`).Scan(&botUser); err != nil {
		t.Fatal(err)
	}
	if _, err := w.p.Exec(ctx, `INSERT INTO bots (user_id, name) VALUES ($1, 'm')`, botUser); err != nil {
		t.Fatal(err)
	}
	if err := w.p.QueryRow(ctx, `INSERT INTO roles (name, position, managed, bot_user_id) VALUES ('m', 1, TRUE, $1) RETURNING id`, botUser).Scan(&role); err != nil {
		t.Fatal(err)
	}
	return role
}

// allowBotRole runs AllowBotRoleInChannel in a transaction of its own and
// commits only when it succeeds, like the install does.
func allowBotRole(t *testing.T, w *world, role, ch uuid.UUID, allow perm.Set) error {
	t.Helper()
	ctx := context.Background()
	tx, err := w.p.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := perm.AllowBotRoleInChannel(ctx, tx, role, ch, allow); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func overwriteOf(t *testing.T, w *world, ch, role uuid.UUID) (allow, deny int64, found bool) {
	t.Helper()
	err := w.p.QueryRow(context.Background(), `SELECT allow, deny FROM permission_overwrites WHERE channel_id = $1 AND role_id = $2`, ch, role).Scan(&allow, &deny)
	return allow, deny, err == nil
}

// Installing from a manifest widens only a bot's own role, only with
// channel-scoped permissions and never on a temporary Talk; each refusal
// leaves no overwrite behind.
func TestABotsOwnRoleIsTheOnlyRoleAManifestInstallCanAllowInAChannel(t *testing.T) {
	w := newWorld(t)
	role := botWithOwnRole(t, w)
	lobby, talk := lobbyWithTalk(t, w)

	wantAPI(t, "a permission outside the channel scope", allowBotRole(t, w, role, w.channel, perm.All&^perm.ChannelScoped), http.StatusBadRequest)
	if err := allowBotRole(t, w, w.everyone, w.channel, perm.SendMessages); err == nil || apiCode(err) != 0 {
		t.Fatalf("@everyone is no bot's own role: want an internal error, got %v", err)
	}
	if err := allowBotRole(t, w, uuid.New(), w.channel, perm.SendMessages); err == nil || apiCode(err) != 0 {
		t.Fatalf("an unknown role: want an internal error, got %v", err)
	}
	wantAPI(t, "a temporary Talk", allowBotRole(t, w, role, talk, perm.Connect), http.StatusConflict)
	for _, target := range []struct {
		name     string
		ch, role uuid.UUID
	}{{"channel", w.channel, role}, {"@everyone", w.channel, w.everyone}, {"Talk", talk, role}} {
		if _, _, found := overwriteOf(t, w, target.ch, target.role); found {
			t.Fatalf("a refusal wrote an overwrite (%s)", target.name)
		}
	}

	// Allowed on the lobby, the change reaches the Talk opened from it, and
	// an allow lifts an earlier deny of the same bit.
	if _, err := w.p.Exec(context.Background(), `INSERT INTO permission_overwrites (channel_id, role_id, allow, deny) VALUES ($1, $2, 0, $3)`, lobby, role, int64(perm.Connect|perm.Speak)); err != nil {
		t.Fatal(err)
	}
	if err := allowBotRole(t, w, role, lobby, perm.Connect); err != nil {
		t.Fatalf("the bot's own role on the lobby: %v", err)
	}
	for _, ch := range []uuid.UUID{lobby, talk} {
		allow, deny, found := overwriteOf(t, w, ch, role)
		if !found || allow != int64(perm.Connect) || deny != int64(perm.Speak) {
			t.Fatalf("overwrite on %v: allow %d deny %d found %v", ch, allow, deny, found)
		}
	}
}
