//go:build integration

package perm_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
)

// A lobby with one temporary Talk opened from it.
func lobbyWithTalk(t *testing.T, w *world) (lobby, talk uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	if err := w.p.QueryRow(ctx, `INSERT INTO channels (name, type, is_lobby) VALUES ('lobby', 'voice', TRUE) RETURNING id`).Scan(&lobby); err != nil {
		t.Fatal(err)
	}
	if err := w.p.QueryRow(ctx, `INSERT INTO channels (name, type, is_temp, lobby_id) VALUES ('Talk 1', 'voice', TRUE, $1) RETURNING id`, lobby).Scan(&talk); err != nil {
		t.Fatal(err)
	}
	return lobby, talk
}

func TestTemporaryTalksFollowTheirLobbysOverwrites(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	lobby, talk := lobbyWithTalk(t, w)
	list := []perm.Overwrite{{Type: "role", ID: w.everyone, Deny: perm.Connect}}

	wantAPI(t, "editing a temporary Talk directly", w.svc.SetOverwrites(ctx, w.owner, perm.TargetChannel, talk, list), http.StatusConflict)
	if err := w.svc.SetOverwrites(ctx, w.owner, perm.TargetChannel, lobby, list); err != nil {
		t.Fatal(err)
	}
	var deny int64
	if err := w.p.QueryRow(ctx, `SELECT deny FROM permission_overwrites WHERE channel_id = $1`, talk).Scan(&deny); err != nil || deny != int64(perm.Connect) {
		t.Fatalf("the Talk mirrors the lobby: %d %v", deny, err)
	}
	// A channel with no Talks opened from it syncs nothing, and a category is no channel to guard.
	if err := w.svc.SetOverwrites(ctx, w.owner, perm.TargetChannel, w.channel, list); err != nil {
		t.Fatalf("an ordinary channel: %v", err)
	}
	if err := w.svc.SetOverwrites(ctx, w.owner, perm.TargetCategory, w.category, list); err != nil {
		t.Fatalf("a category: %v", err)
	}
}

// If the copy to a Talk fails, the lobby change fails with it and nothing is half written.
func TestAFailedCopyToATemporaryTalkRollsTheLobbyChangeBack(t *testing.T) {
	for _, tc := range []struct{ name, when, want string }{
		{"clearing", "DELETE", "clear temporary Talk overwrites"},
		{"copying", "INSERT", "copy lobby overwrites"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newWorld(t)
			ctx := context.Background()
			lobby, talk := lobbyWithTalk(t, w)
			if _, err := w.p.Exec(ctx, `INSERT INTO permission_overwrites (channel_id, role_id, deny) VALUES ($1, $2, 1)`, talk, w.everyone); err != nil {
				t.Fatal(err)
			}
			// Only the statement under test touches the Talk's rows in a way that fires the trigger.
			trigger := `
				CREATE FUNCTION fail_talk_overwrites() RETURNS trigger AS $$
				BEGIN
					IF COALESCE(NEW.channel_id, OLD.channel_id) = '` + talk.String() + `' THEN RAISE EXCEPTION 'boom'; END IF;
					RETURN COALESCE(NEW, OLD);
				END $$ LANGUAGE plpgsql;
				CREATE TRIGGER fail_talk BEFORE ` + tc.when + ` ON permission_overwrites FOR EACH ROW EXECUTE FUNCTION fail_talk_overwrites()`
			if _, err := w.p.Exec(ctx, trigger); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				_, _ = w.p.Exec(context.Background(), `DROP TRIGGER IF EXISTS fail_talk ON permission_overwrites; DROP FUNCTION IF EXISTS fail_talk_overwrites()`)
			})
			list := []perm.Overwrite{{Type: "role", ID: w.everyone, Deny: perm.Connect}}
			err := w.svc.SetOverwrites(ctx, w.owner, perm.TargetChannel, lobby, list)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v; want %q", err, tc.want)
			}
			var n int
			_ = w.p.QueryRow(ctx, `SELECT count(*) FROM permission_overwrites WHERE channel_id = $1`, lobby).Scan(&n)
			if n != 0 {
				t.Fatalf("the lobby change was kept: %d overwrites", n)
			}
		})
	}
}
