//go:build integration

package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/testutil/racetest"
)

// Lane 7 of the defect hunt: an edit and a delete of one message released
// together. doc/webhooks.md: events are taken "in the order their changes were
// written (two changes of the same message in the order they took effect)".
// Oracle per round: the message is gone when the delete answered 204; an
// edit that answered 200 recorded MESSAGE_UPDATE before the MESSAGE_DELETE;
// no MESSAGE_UPDATE follows the MESSAGE_DELETE.
func TestAnEditAndADeleteOfOneMessageAgreeWithTheWebhookOrder(t *testing.T) {
	a := newApp(t, false)
	admin := a.seedAdmin()
	ch := a.createChannel(admin, "general", "text")
	ctx := context.Background()
	// Through the route, so the dispatcher's list of webhooks knows it.
	wantStatus(t, "outgoing webhook", admin.post("/api/admin/channels/"+ch.String()+"/outgoing-webhooks", map[string]any{
		"name": "order", "url": "https://hooks.example.org/x", "events": []string{"MESSAGE_UPDATE", "MESSAGE_DELETE"},
	}), http.StatusCreated)
	// The dispatcher may claim and settle rows; a log written by a trigger
	// keeps the order they were written in.
	a.runSQL(`CREATE TABLE IF NOT EXISTS lane7_queue_log (seq BIGSERIAL PRIMARY KEY, event_type TEXT, data BYTEA)`)
	a.runSQL(`CREATE OR REPLACE FUNCTION lane7_log() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN INSERT INTO lane7_queue_log (event_type, data) VALUES (NEW.event_type, NEW.data); RETURN NEW; END $$`)
	a.runSQL(`CREATE TRIGGER lane7_log AFTER INSERT ON outgoing_queue FOR EACH ROW EXECUTE FUNCTION lane7_log()`)
	t.Cleanup(func() {
		_, _ = a.db.Exec(ctx, `DROP TRIGGER IF EXISTS lane7_log ON outgoing_queue; DROP FUNCTION IF EXISTS lane7_log(); DROP TABLE IF EXISTS lane7_queue_log`)
	})
	var msg uuid.UUID
	edits := 0
	// The deep count stays under the member's write rate (about 300 writes a
	// minute), which would otherwise answer 429.
	racetest.Sweep(t, racetest.Rounds(30, 250), 2,
		func(round int) {
			// With SQL: the message rate would refuse a sweep of many rounds.
			if err := a.db.QueryRow(ctx, `INSERT INTO messages (channel_id, user_id, content) VALUES ($1, $2, $3) RETURNING id`,
				ch, admin.user.ID, fmt.Sprintf("round %d", round)).Scan(&msg); err != nil {
				t.Fatal(err)
			}
			a.runSQL(`DELETE FROM lane7_queue_log`)
		},
		func(round, i int) error {
			path := "/api/channels/" + ch.String() + "/messages/" + msg.String()
			if i == 0 {
				return statusErr(admin.put(path, map[string]any{"content": "edited"}), http.StatusOK)
			}
			return statusErr(admin.delete(path), http.StatusNoContent)
		},
		func(round int, r racetest.Round) string {
			if r.Errs[1] != nil {
				return fmt.Sprintf("round %d: the delete failed: %v", round, r.Errs[1])
			}
			if n := a.count(`SELECT COUNT(*) FROM messages WHERE id = $1`, msg); n != 0 {
				return fmt.Sprintf("round %d: the deleted message is still there", round)
			}
			rows, err := a.db.Query(ctx, `SELECT event_type FROM lane7_queue_log ORDER BY seq`)
			if err != nil {
				t.Fatal(err)
			}
			var order []string
			for rows.Next() {
				var e string
				if err := rows.Scan(&e); err != nil {
					t.Fatal(err)
				}
				order = append(order, e)
			}
			if r.Errs[0] == nil {
				edits++
			} else if !strings.HasPrefix(r.Errs[0].Error(), "404 ") {
				return fmt.Sprintf("round %d: the edit failed otherwise than as not found: %v", round, r.Errs[0])
			}
			want := []string{"MESSAGE_DELETE"}
			if r.Errs[0] == nil {
				want = []string{"MESSAGE_UPDATE", "MESSAGE_DELETE"}
			}
			if fmt.Sprint(order) != fmt.Sprint(want) {
				return fmt.Sprintf("round %d: edit answer %v, webhook rows %v", round, r.Errs[0], order)
			}
			return ""
		}).FailOnViolation(t)
	t.Logf("edits that won the race: %d", edits)
}
