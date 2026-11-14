package push

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/mention"
)

// mentionLabels maps the lower-cased usernames mentioned in content to the
// members' display names, so a push reads "@Mnemo" and not "@bot-3f9a...".
// Members without a display name, and any read failure, leave the username.
func mentionLabels(ctx context.Context, q db.Querier, content string) map[string]string {
	names := mention.Names(content)
	if len(names) == 0 {
		return nil
	}
	var out map[string]string
	rows, err := q.Query(ctx, `SELECT lower(username), display_name FROM users WHERE lower(username) = ANY($1) AND display_name <> ''`, names)
	if err == nil {
		out, err = collectLabels(rows)
	}
	if err != nil {
		slog.Warn("push: load mention names", "err", err)
		return nil
	}
	return out
}

func collectLabels(rows pgx.Rows) (map[string]string, error) {
	pairs, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (p [2]string, err error) {
		err = row.Scan(&p[0], &p[1])
		return p, err
	})
	out := make(map[string]string, len(pairs))
	for _, p := range pairs {
		out[p[0]] = p[1]
	}
	return out, err
}
