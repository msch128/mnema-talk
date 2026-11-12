//go:build integration

package server

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/bot"
	"github.com/msch128/mnema-talk/internal/gateway"
)

// The bot rows of the revocation matrix: a bot holds a live gateway session
// on the channel its role lets it see; each row takes one right away and
// measures when the session closes or the channel's events stop reaching it.
// Bounds: token revocation, regeneration, disabling and deleting close at once
// (internal/gateway/gateway_tokens.go); a token that ends without the API
// (expiry) closes within tokenCheckEvery, 30 s (internal/gateway/gateway.go:74,
// 200 ms in this test); a permission change stops the events at once because
// Observe filters every event on the current snapshot
// (internal/gateway/observe.go:65-103).
func TestRevocationMatrixForBots(t *testing.T) {
	type want int
	const (
		closes want = iota
		silent      // the session stays open, the channel's events stop
	)
	rows := []struct {
		name   string
		revoke func(*testing.T, *botFixture)
		want   want
	}{
		{"token revoked", func(t *testing.T, f *botFixture) {
			wantStatus(t, "revoke", f.owner.delete(f.botPath+"/tokens/"+f.token.ID.String()), http.StatusNoContent)
		}, closes},
		{"token replaced", func(t *testing.T, f *botFixture) {
			wantStatus(t, "regenerate", f.owner.post(f.botPath+"/tokens/"+f.token.ID.String()+"/regenerate", nil), http.StatusCreated)
		}, closes},
		{"token expired", func(t *testing.T, f *botFixture) {
			if _, err := f.a.db.Exec(t.Context(), `UPDATE bot_tokens SET expires_at = NOW() - INTERVAL '1 second' WHERE id = $1`, f.token.ID); err != nil {
				t.Fatal(err)
			}
		}, closes},
		{"bot disabled", func(t *testing.T, f *botFixture) {
			wantStatus(t, "disable", f.owner.patch(f.botPath, map[string]any{"disabled": true}), http.StatusOK)
		}, closes},
		{"bot removed", func(t *testing.T, f *botFixture) {
			wantStatus(t, "delete", f.owner.delete(f.botPath), http.StatusNoContent)
		}, closes},
		{"bot's role taken away", func(t *testing.T, f *botFixture) {
			wantStatus(t, "roles", f.owner.put(f.botPath+"/roles", map[string]any{"role_ids": []uuid.UUID{}}), http.StatusOK)
		}, silent},
		{"channel overwrite changed", func(t *testing.T, f *botFixture) {
			wantStatus(t, "hide", putOverwrites(f.owner, "channels", f.open), http.StatusNoContent)
		}, silent},
		{"channel deleted", func(t *testing.T, f *botFixture) {
			wantStatus(t, "delete channel", f.owner.delete("/api/admin/channels/"+f.open.String()), http.StatusNoContent)
		}, silent},
	}
	for _, row := range rows {
		t.Run(strings.ReplaceAll(row.name, " ", "_"), func(t *testing.T) {
			f := gwFixture(t)
			f.a.router.Gateway.SetTimings(gateway.Timings{Heartbeat: time.Minute, Identify: 10 * time.Second, Resume: time.Minute, TokenCheck: 200 * time.Millisecond})
			g := f.dialGW("Bot " + f.token.Secret)
			g.identify("", bot.IntentGuildMessages|bot.IntentGuilds)
			postMessage(t, f.owner, f.open, map[string]any{"content": "before"})
			g.expect("MESSAGE_CREATE")

			start := time.Now()
			row.revoke(t, f)
			switch row.want {
			case closes:
				g.waitClose(gateway.CloseAuthFailed)
				t.Logf("%s: closed after %v", row.name, time.Since(start).Round(time.Millisecond))
			case silent:
				// A canary in the channel (when it still exists) must not arrive.
				if res := f.owner.post("/api/channels/"+f.open.String()+"/messages", map[string]any{"content": "after-the-change"}); res.status != http.StatusCreated && res.status != http.StatusNotFound {
					t.Fatalf("canary: %d %s", res.status, res.body)
				}
				for {
					name, _, d, ok := g.dispatch(quiet)
					if !ok {
						break
					}
					if name == "MESSAGE_CREATE" && strings.Contains(string(d), "after-the-change") {
						t.Fatalf("%s: the bot still received the channel's message %v later", row.name, time.Since(start))
					}
				}
			}
		})
	}
}
