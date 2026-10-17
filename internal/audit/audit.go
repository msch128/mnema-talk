// Package audit keeps the append-only record of administrative, moderation
// and security-relevant events (doc/moderation.md).
//
// Every entry names who did what to which target, with a sanitized
// before/after view, the time and the request id. Secrets never reach it:
// passwords, tokens, secrets, hashes, invite codes and session ids are
// redacted by key, and URLs lose their query and credentials. Message
// deletions and edits do keep the message text, because an audit log that
// cannot say what was removed is of little use; the operator must know that
// (SECURITY.md). Entries are inserted once and never changed; the database
// refuses updates. Only the optional retention job (AUDIT_RETENTION_DAYS,
// default 0 = keep forever) deletes old rows.
package audit

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// Entry is one thing that happened.
type Entry struct {
	// ActorID and ActorName default to the signed-in user of the context.
	ActorID    *uuid.UUID
	ActorName  string
	Action     string
	TargetType string
	TargetID   string
	TargetName string
	// Details is sanitized before it is stored.
	Details map[string]any
	// RequestID defaults to the id of the request in the context.
	RequestID string
}

// Log writes and reads the audit log. A nil *Log records nothing, so
// components can run without one in tests.
type Log struct {
	DB db.Querier
	// Actor names the signed-in user of a context.
	Actor func(ctx context.Context) (id uuid.UUID, name string, ok bool)
	// Privileged says whether the signed-in user is the Owner or an
	// Administrator; only they read the sensitive entries (see Sensitive).
	// Without it nobody does.
	Privileged func(ctx context.Context) (bool, error)
}

// Sensitive lists the action patterns (SQL LIKE) that only the Owner and
// Administrators may read, even with the View audit log permission: webhooks
// (their addresses are credentials), bots, system and media administration,
// settings, and the Owner-only account takeovers.
var Sensitive = []string{
	"webhook.%", "outgoing_webhook.%", "bot.%", "system.%", "settings.%", "media.%", "admin.%",
	"user.password_set", "user.password_reset", "user.lockout_clear",
}

// Record stores e. It returns the database error; callers that already did
// the work log it instead of failing the request (see RecordOrLog).
func (l *Log) Record(ctx context.Context, e Entry) error {
	if l == nil {
		return nil
	}
	return l.RecordTx(ctx, l.DB, e)
}

// RecordTx stores e through q, for callers that write the entry in the same
// transaction as the action itself.
func (l *Log) RecordTx(ctx context.Context, q db.Querier, e Entry) error {
	if l == nil {
		return nil
	}
	if e.ActorID == nil && l.Actor != nil {
		if id, name, ok := l.Actor(ctx); ok {
			e.ActorID, e.ActorName = &id, name
		}
	}
	if e.RequestID == "" {
		e.RequestID = httpx.RequestIDFromContext(ctx)
	}
	// SanitizeDetails returns only JSON-safe values, so this cannot fail.
	details, _ := json.Marshal(SanitizeDetails(e.Details))
	_, err := q.Exec(ctx, `
		INSERT INTO audit_log (actor_id, actor_name, action, target_type, target_id, target_name, details, request_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		e.ActorID, clip(e.ActorName, 64), e.Action, e.TargetType, clip(e.TargetID, 64), clip(e.TargetName, 120), details, clip(e.RequestID, 64))
	return err
}

// RecordOrLog records e and logs a failure: the action it describes already
// happened and must not be reported as failed.
func (l *Log) RecordOrLog(ctx context.Context, e Entry) {
	if err := l.Record(ctx, e); err != nil {
		slog.ErrorContext(ctx, "audit log write failed", "action", e.Action, "err", err)
	}
}

// Prune deletes entries older than days; days <= 0 keeps everything.
func (l *Log) Prune(ctx context.Context, days int) (int64, error) {
	if l == nil || days <= 0 {
		return 0, nil
	}
	tag, err := l.DB.Exec(ctx, `DELETE FROM audit_log WHERE created_at < NOW() - make_interval(days => $1)`, days)
	return tag.RowsAffected(), err
}

// RunRetention prunes every interval until ctx ends. With days <= 0 it
// returns at once: nothing is ever deleted automatically.
func (l *Log) RunRetention(ctx context.Context, days int, every time.Duration) {
	if l == nil || days <= 0 {
		return
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if n, err := l.Prune(ctx, days); err != nil {
			slog.Error("audit log retention failed", "err", err)
		} else if n > 0 {
			slog.Info("audit log retention", "deleted", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
