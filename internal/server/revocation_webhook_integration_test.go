//go:build integration

package server

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// Queued outgoing deliveries after the thing they stood on was taken away:
// the first attempt fails, and before the retry is due the row's change
// happens. The retry runs 300 ms later.
func TestQueuedWebhookDeliveriesAfterARevocation(t *testing.T) {
	rows := []struct {
		name   string
		revoke func(t *testing.T, a *app, admin *client, hook, ch, msg string)
		// delivered is what the retry may still send, by design.
		delivered bool
	}{
		{"webhook deleted", func(t *testing.T, _ *app, admin *client, hook, _, _ string) {
			wantStatus(t, "delete hook", admin.delete("/api/admin/outgoing-webhooks/"+hook), http.StatusNoContent)
		}, false},
		{"webhook switched off", func(t *testing.T, _ *app, admin *client, hook, _, _ string) {
			wantStatus(t, "switch off", admin.patch("/api/admin/outgoing-webhooks/"+hook, map[string]any{"enabled": false}), http.StatusOK)
		}, false},
		{"channel deleted", func(t *testing.T, _ *app, admin *client, _, ch, _ string) {
			wantStatus(t, "delete channel", admin.delete("/api/admin/channels/"+ch), http.StatusNoContent)
		}, false},
		// The event was published while the message existed; the receiver is
		// the admin's own endpoint. Whether a later delete recalls it is
		// measured here and reported.
		{"message deleted", func(t *testing.T, _ *app, admin *client, _, ch, msg string) {
			wantStatus(t, "delete message", admin.delete("/api/channels/"+ch+"/messages/"+msg), http.StatusNoContent)
		}, true},
	}
	for _, row := range rows {
		t.Run(strings.ReplaceAll(row.name, " ", "_"), func(t *testing.T) {
			a := outgoingApp(t)
			a.router.Webhooks.Outgoing.SetBackoff([]time.Duration{300 * time.Millisecond})
			admin := a.seedAdmin()
			ch := a.createChannel(admin, "alerts", "text")
			rc := newReceiver(t, func(n int, _ *http.Request) int {
				if n == 1 {
					return http.StatusServiceUnavailable
				}
				return http.StatusNoContent
			})
			o := makeOutgoing(t, admin, ch, rc.srv.URL, "MESSAGE_CREATE")
			msg := a.send(admin, ch, "queued-canary")
			rc.next(t) // the failing first attempt
			row.revoke(t, a, admin, o.ID.String(), ch.String(), msg.String())
			select {
			case got := <-rc.got:
				if !row.delivered {
					t.Fatalf("%s: the queued delivery was still sent: %s", row.name, got.body)
				}
				t.Logf("%s: retried after the change (by design): %v", row.name, strings.Contains(string(got.body), "queued-canary"))
			case <-time.After(1500 * time.Millisecond):
				if row.delivered {
					t.Logf("%s: the queued delivery was dropped", row.name)
				}
			}
		})
	}
}
