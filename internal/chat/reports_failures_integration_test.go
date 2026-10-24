//go:build integration

package chat

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestFilingAReportReportsDatabaseFailures(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	var reporter uuid.UUID
	if err := f.p.QueryRow(ctx, `INSERT INTO users (username, display_name, password_hash) VALUES ('reporter_one', 'Reporter', 'x') RETURNING id`).Scan(&reporter); err != nil {
		t.Fatal(err)
	}
	nr := NewReport{Reporter: reporter, MessageID: f.m, Category: "spam"}

	cases := map[string]struct {
		views map[string]string
		want  string
	}{
		"channel lookup":    {map[string]string{"channels": `SELECT id FROM public.channels`}, "load report channel"},
		"attachments":       {map[string]string{"media": `SELECT NULL::uuid AS message_id`}, "report context"},
		"open report probe": {map[string]string{"message_reports": `SELECT NULL::uuid AS id`}, "reporter_id"},
		"open report count": {map[string]string{"message_reports": `SELECT NULL::uuid AS id, NULL::uuid AS reporter_id, NULL::uuid AS message_id`}, "status"},
		"insert":            {map[string]string{"message_reports": `SELECT NULL::uuid AS id, NULL::uuid AS reporter_id, NULL::uuid AS message_id, NULL::text AS status`}, "message_reports"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := FileReport(ctx, driftPool(t, f.p, c.views), nr)
			requireChatError(t, err, c.want)
		})
	}
}
