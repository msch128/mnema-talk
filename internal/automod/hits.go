package automod

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
)

// ageCache remembers when accounts were created: the creation time never
// changes, so the send path asks the database about a member once.
type ageCache struct {
	mu sync.Mutex
	m  map[uuid.UUID]time.Time
}

const ageCacheMax = 8192

func (c *ageCache) createdAt(ctx context.Context, q db.Querier, id uuid.UUID) (time.Time, error) {
	c.mu.Lock()
	t, ok := c.m[id]
	c.mu.Unlock()
	if ok {
		return t, nil
	}
	if err := q.QueryRow(ctx, `SELECT created_at FROM users WHERE id = $1`, id).Scan(&t); err != nil {
		return t, err
	}
	c.mu.Lock()
	if c.m == nil || len(c.m) >= ageCacheMax {
		c.m = make(map[uuid.UUID]time.Time, 64)
	}
	c.m[id] = t
	c.mu.Unlock()
	return t, nil
}

// snapshotMessage is the copy a queue entry keeps (the shape of
// chat.ReportSnapshot): the message only, never context, and for a direct
// message no text at all.
type snapshotMessage struct {
	ID          uuid.UUID `json:"id"`
	UserID      uuid.UUID `json:"user_id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name"`
	Content     string    `json:"content"`
	Attachments []any     `json:"attachments"`
	CreatedAt   time.Time `json:"created_at"`
}

// record adds a hit to the moderation queue and applies the rule's timeout.
// It never fails the message: the queue is a best-effort record, logged when
// it cannot be written.
func (e *Engine) record(ctx context.Context, p Post, dm bool, r *compiled, outcome string, message uuid.UUID) {
	if err := e.insertHit(ctx, p, dm, r, outcome, message); err != nil {
		slog.Error("automod hit not recorded", "rule", r.Name, "err", err)
	}
	if r.TimeoutSeconds > 0 {
		e.timeout(ctx, p.UserID, r.TimeoutSeconds, "Automod: "+r.Name)
	}
}

func (e *Engine) insertHit(ctx context.Context, p Post, dm bool, r *compiled, outcome string, message uuid.UUID) error {
	var username, display, channelName string
	err := e.DB.QueryRow(ctx, `
		SELECT u.username, u.display_name, c.name FROM users u, channels c WHERE u.id = $1 AND c.id = $2`,
		p.UserID, p.ChannelID).Scan(&username, &display, &channelName)
	if err != nil {
		return err
	}
	kind, content := "channel", p.Content
	if dm {
		// Nobody but the members read a direct message: the entry says who
		// broke which rule and where, never what was written.
		kind, channelName, content = "dm", "", ""
	}
	raw, err := json.Marshal(map[string]any{
		"message": snapshotMessage{ID: message, UserID: p.UserID, Username: username, DisplayName: display, Content: content, Attachments: []any{}, CreatedAt: time.Now().UTC()},
		"context": []any{},
	})
	if err != nil {
		return err
	}
	var msgID *uuid.UUID
	if message != uuid.Nil {
		msgID = &message
	}
	// One open entry per member and rule: further hits only count up.
	_, err = e.DB.Exec(ctx, `
		INSERT INTO message_reports (reporter_id, reported_user_id, message_id, channel_id, kind, channel_name, category, snapshot,
		                             source, automod_rule, automod_outcome)
		VALUES (NULL, $1, $2, $3, $4, $5, 'automod', $6, 'automod', $7, $8)
		ON CONFLICT (reported_user_id, automod_rule) WHERE source = 'automod' AND status = 'open' AND reported_user_id IS NOT NULL
		DO UPDATE SET hit_count = message_reports.hit_count + 1`,
		p.UserID, msgID, p.ChannelID, kind, channelName, raw, r.Name, outcome)
	return err
}

// timeout silences the member for seconds (or leaves a longer timeout alone)
// and tells them and the clients, like a moderator's timeout does.
func (e *Engine) timeout(ctx context.Context, userID uuid.UUID, seconds int, reason string) {
	until, err := e.setTimeout(ctx, userID, seconds, reason)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Error("automod timeout failed", "err", err)
		}
		return
	}
	e.announceTimeouts(map[uuid.UUID]time.Time{userID: until}, seconds, reason)
	e.Audit.RecordOrLog(context.Background(), audit.Entry{
		ActorName: "Automod", Action: "automod.timeout", TargetType: "user", TargetID: userID.String(),
		Details: map[string]any{"duration_seconds": seconds, "reason": reason},
	})
}

// announceTimeouts tells the members and the clients about timeouts that were
// just stored (id to end time), for both the message rules and raid protection.
// The notice carries the time left, which is longer than seconds when a longer
// timeout was already running.
func (e *Engine) announceTimeouts(untils map[uuid.UUID]time.Time, seconds int, reason string) {
	ids := make([]uuid.UUID, 0, len(untils))
	for id := range untils {
		ids = append(ids, id)
	}
	e.Perms.Changed(ids)
	if e.Events == nil {
		return
	}
	for id, until := range untils {
		left := max(seconds, int(math.Ceil(time.Until(until).Seconds())))
		e.Events.SendToUsers([]uuid.UUID{id}, events.ModerationNotice, map[string]any{
			"kind": "timeout", "until": until, "duration_seconds": left, "reason": reason,
		})
		e.Events.Broadcast(events.MemberTimeout, map[string]any{"id": id, "timeout_until": until})
	}
}

// setTimeout stores the timeout; the Owner, Administrators' accounts (role
// 'admin') and bots are never timed out. It returns the end time. A timeout
// that runs longer already stays as it is, with its reason and moderator.
func (e *Engine) setTimeout(ctx context.Context, userID uuid.UUID, seconds int, reason string) (time.Time, error) {
	var until time.Time
	err := e.DB.QueryRow(ctx, `
		UPDATE users SET timeout_reason = CASE WHEN NOW() + make_interval(secs => $2) > COALESCE(timeout_until, NOW()) THEN LEFT($3, 200) ELSE timeout_reason END,
		       timeout_by = CASE WHEN NOW() + make_interval(secs => $2) > COALESCE(timeout_until, NOW()) THEN NULL ELSE timeout_by END,
		       timeout_until = GREATEST(COALESCE(timeout_until, NOW()), NOW() + make_interval(secs => $2)), updated_at = NOW()
		WHERE id = $1 AND role <> 'admin' AND NOT is_bot AND deleted_at IS NULL
		RETURNING timeout_until`, userID, float64(seconds), reason).Scan(&until)
	if err != nil {
		return until, fmt.Errorf("automod timeout: %w", err)
	}
	return until, nil
}
