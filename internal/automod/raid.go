package automod

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/events"
)

const raidReason = "Automod: raid protection"

// OnJoin tells the engine that userID just joined. When more accounts than the
// raid rule allows joined within its window, the newcomers of the window and
// every one who joins while the raid lasts are timed out for the rule's
// restriction time, and one entry in the moderation queue announces it.
// Moderators lift a timeout like any other. It never fails the sign-up.
func (e *Engine) OnJoin(ctx context.Context, userID uuid.UUID) {
	if e == nil {
		return
	}
	set, err := e.current(ctx)
	if err != nil || set.raid == nil {
		if err != nil {
			slog.Error("automod rules", "err", err)
		}
		return
	}
	restricted, started, until, err := e.restrictJoiners(ctx, set.raid, userID)
	if err != nil {
		slog.Error("automod raid check failed", "err", err)
		return
	}
	if len(restricted) == 0 {
		return
	}
	e.Perms.Changed(restricted)
	if e.Events != nil {
		e.Events.SendToUsers(restricted, events.ModerationNotice, map[string]any{
			"kind": "timeout", "until": until, "duration_seconds": set.raid.Config.RestrictSeconds, "reason": raidReason,
		})
		for _, id := range restricted {
			e.Events.Broadcast(events.MemberTimeout, map[string]any{"id": id, "timeout_until": until})
		}
	}
	if started {
		e.Audit.RecordOrLog(context.Background(), audit.Entry{
			ActorName: "Automod", Action: "automod.raid", TargetType: "automod_rule", TargetID: set.raid.ID.String(), TargetName: set.raid.Name,
			Details: map[string]any{"restricted": len(restricted), "restrict_seconds": set.raid.Config.RestrictSeconds},
		})
	}
}

// restrictJoiners decides, under the lock of the state row, whether a raid is
// on, times out the joiners it covers and keeps the queue entry. started says
// that this join began the raid.
func (e *Engine) restrictJoiners(ctx context.Context, r *compiled, userID uuid.UUID) (restricted []uuid.UUID, started bool, until time.Time, err error) {
	err = pgx.BeginFunc(ctx, e.DB, func(tx pgx.Tx) error {
		var active bool
		var report *uuid.UUID
		var joins int
		if err := tx.QueryRow(ctx, `
			SELECT COALESCE(raid_until > NOW(), FALSE), raid_report,
			       (SELECT COUNT(*) FROM users WHERE created_at > NOW() - make_interval(secs => $1) AND NOT is_bot AND deleted_at IS NULL)
			FROM automod_state FOR UPDATE`, float64(r.Config.WindowSeconds)).Scan(&active, &report, &joins); err != nil {
			return err
		}
		if !active && joins <= r.Config.Joins {
			return nil
		}
		started = !active
		rows, err := tx.Query(ctx, `
			UPDATE users SET timeout_until = GREATEST(COALESCE(timeout_until, NOW()), NOW() + make_interval(secs => $3)),
			       timeout_reason = $4, timeout_by = NULL, updated_at = NOW()
			WHERE role <> 'admin' AND NOT is_bot AND deleted_at IS NULL
			  AND (id = $1 OR ($2 AND created_at > NOW() - make_interval(secs => $5)))
			RETURNING id, timeout_until`, userID, started, float64(r.Config.RestrictSeconds), raidReason, float64(r.Config.WindowSeconds))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id, &until); err != nil {
				return err
			}
			restricted = append(restricted, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		return e.noteRaid(ctx, tx, r, started, report, len(restricted))
	})
	return restricted, started, until, err
}

// noteRaid creates the queue entry of a raid that just began, or counts one
// more restricted joiner on the entry of the raid that is running.
func (e *Engine) noteRaid(ctx context.Context, tx pgx.Tx, r *compiled, started bool, report *uuid.UUID, n int) error {
	if !started {
		if _, err := tx.Exec(ctx, `UPDATE automod_state SET raid_until = NOW() + make_interval(secs => $1)`, float64(r.Config.RestrictSeconds)); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE message_reports SET hit_count = hit_count + $2 WHERE id = $1`, report, n)
		return err
	}
	raw, err := json.Marshal(map[string]any{
		"message": snapshotMessage{Attachments: []any{}, CreatedAt: time.Now().UTC()},
		"context": []any{},
	})
	if err != nil {
		return err
	}
	var id uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO message_reports (kind, category, snapshot, source, automod_rule, automod_outcome, hit_count)
		VALUES ('alert', 'automod', $1, 'automod', $2, 'alert', $3) RETURNING id`, raw, r.Name, n).Scan(&id); err != nil {
		return fmt.Errorf("raid alert: %w", err)
	}
	_, err = tx.Exec(ctx, `UPDATE automod_state SET raid_until = NOW() + make_interval(secs => $1), raid_report = $2`, float64(r.Config.RestrictSeconds), id)
	return err
}
