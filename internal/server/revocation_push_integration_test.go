//go:build integration

package server

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/perm"
)

// A push held in the coalescing window when the member loses the channel or
// the account: the window's end re-checks access (internal/push/service.go:
// 263-302), so nothing more is sent. The window is 400 ms here (5 s by
// default).
func TestAHeldPushIsDroppedWhenTheRightGoesInsideTheWindow(t *testing.T) {
	rows := []struct {
		name   string
		revoke func(t *testing.T, owner, bob *client, ch uuid.UUID)
	}{
		{"nothing changes (control: the held push is sent)", nil},
		{"role removed", func(t *testing.T, owner, bob *client, _ uuid.UUID) { assignRoles(t, owner, bob) }},
		{"channel made private", func(t *testing.T, owner, _ *client, ch uuid.UUID) {
			wantStatus(t, "hide", putOverwrites(owner, "channels", ch, overwrite("role", everyoneID(t, owner), 0, perm.ViewChannel)), http.StatusNoContent)
		}},
		{"banned", func(t *testing.T, owner, bob *client, _ uuid.UUID) {
			wantStatus(t, "ban", owner.post("/api/admin/users/"+bob.user.ID.String()+"/ban", map[string]any{"reason": ""}), http.StatusNoContent)
		}},
		{"account disabled", func(t *testing.T, owner, bob *client, _ uuid.UUID) {
			wantStatus(t, "disable", owner.post("/api/admin/users/"+bob.user.ID.String()+"/disable", nil), http.StatusNoContent)
		}},
	}
	for _, row := range rows {
		t.Run(strings.ReplaceAll(row.name, " ", "_"), func(t *testing.T) {
			fake := newFakePushService(t)
			a := newAppWithDeps(t, true, func(*config.Config) {}, func(d *Deps) {
				d.PushClient = fake
				d.PushBurst = 400 * time.Millisecond
			})
			owner := a.seedAdmin()
			bob := a.register(owner, "bob")
			staff := createRole(t, owner, "Staff", 0)
			assignRoles(t, owner, bob, staff)
			ch := a.createChannel(owner, "staff", "text")
			wantStatus(t, "private", putOverwrites(owner, "channels", ch,
				overwrite("role", everyoneID(t, owner), 0, perm.ViewChannel), overwrite("role", staff, perm.ViewChannel, 0)), http.StatusNoContent)
			wantStatus(t, "device", bob.post("/api/push/subscriptions", subscription(t, "/bob-phone")), http.StatusCreated)

			a.send(owner, ch, "first")
			fake.waitFor(t, "/bob-phone")
			a.send(owner, ch, "second, held for the window")
			want := 2
			if row.revoke != nil {
				row.revoke(t, owner, bob, ch)
				want = 1
			}
			time.Sleep(time.Second)
			n := 0
			for _, p := range fake.received() {
				if p == "/bob-phone" {
					n++
				}
			}
			if n != want {
				t.Fatalf("%s: %d pushes reached the member's device, want %d", row.name, n, want)
			}
		})
	}
}
