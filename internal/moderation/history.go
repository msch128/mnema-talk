package moderation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// MaxHistoryEvents is how many of the latest events the timeline lists.
const MaxHistoryEvents = 200

var errOwnHistory = httpx.ErrForbidden("you cannot open your own moderation history")

func isNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }

// HistoryEvent is one entry of a member's moderation timeline.
type HistoryEvent struct {
	Kind string    `json:"kind" enums:"timeout,timeout_removed,ban,unban,ban_expired,kick,talk_removal,roles,report,automod"`
	At   time.Time `json:"at" format:"date-time"`
	// Actor is the moderator who did it; "Automod" for automod's own timeout.
	Actor  string `json:"actor,omitempty" binding:"optional"`
	Reason string `json:"reason,omitempty" binding:"optional"`
	// DurationSeconds is the length of a timeout or a ban with an end.
	DurationSeconds int `json:"duration_seconds,omitempty" binding:"optional"`
	// AddedRoles and RemovedRoles name the roles a moderator changed (roles).
	AddedRoles   []string `json:"added_roles,omitempty" binding:"optional"`
	RemovedRoles []string `json:"removed_roles,omitempty" binding:"optional"`
	// ReportID, Category and Status describe a report (report) or an automod hit.
	ReportID *uuid.UUID `json:"report_id,omitempty" binding:"optional" format:"uuid"`
	Category string     `json:"category,omitempty" binding:"optional"`
	Status   string     `json:"status,omitempty" binding:"optional" enums:"open,resolved,dismissed"`
	// Rule, Outcome and Hits describe an automod hit: the rule's name, what it
	// did and how many hits the entry counts.
	Rule    string `json:"rule,omitempty" binding:"optional"`
	Outcome string `json:"outcome,omitempty" binding:"optional" enums:"blocked,flagged,alert"`
	Hits    int    `json:"hits,omitempty" binding:"optional"`
}

// History is the moderation tab of a member.
type History struct {
	// Strikes counts the notes marked as strikes.
	Strikes int `json:"strikes"`
	// Notes are the private moderator notes, newest first.
	Notes []Note `json:"notes"`
	// Events is the timeline, newest first, at most the latest 200.
	Events []HistoryEvent `json:"events"`
}

// auditKinds maps the audit actions of the timeline to its kinds.
var auditKinds = map[string]string{
	"moderation.timeout":        "timeout",
	"automod.timeout":           "timeout",
	"moderation.timeout_remove": "timeout_removed",
	"moderation.ban":            "ban",
	"moderation.unban":          "unban",
	"moderation.ban_expired":    "ban_expired",
	"moderation.kick":           "kick",
	"user.talk_kick":            "talk_removal",
	"user.roles_set":            "roles",
}

// getHistory handles GET /api/admin/users/{id}/moderation.
//
// @Summary A member's moderation history
// @Description The timeline of what moderators and automod did about a member (timeouts, bans, kicks, role changes by moderators, reports about them, automod hits) and the private moderator notes about them. Needs Manage reports; the member themselves never gets it, not even with the permission. A report appears with its category and status only, never with its message text. The Owner and Administrators always pass.
// @ID getModerationHistory
// @Tags Moderation
// @Produce json
// @Security cookieAuth
// @Param id path string true "User ID." Format(uuid)
// @Success 200 {object} History "The history."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Manage reports permission, or the member is yourself."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/users/{id}/moderation [get]
func (h *Handler) getHistory(w http.ResponseWriter, r *http.Request) error {
	id, err := h.subject(r)
	if err != nil {
		return err
	}
	ctx := r.Context()
	notes, err := h.listNotes(ctx, id, auth.UserFrom(ctx))
	if err != nil {
		return err
	}
	logged, err := h.auditEvents(ctx, id)
	if err != nil {
		return err
	}
	reports, err := h.reportEvents(ctx, id)
	if err != nil {
		return err
	}
	events := append(logged, reports...)
	slices.SortStableFunc(events, func(a, b HistoryEvent) int { return b.At.Compare(a.At) })
	if len(events) > MaxHistoryEvents {
		events = events[:MaxHistoryEvents]
	}
	strikes := 0
	for _, n := range notes {
		if n.Strike {
			strikes++
		}
	}
	httpx.WriteJSON(w, http.StatusOK, History{Strikes: strikes, Notes: notes, Events: events})
	return nil
}

// subject reads {id} and checks it names a member other than the caller. Bots
// have no moderation history; an erased account (its tombstone) still has one.
func (h *Handler) subject(r *http.Request) (uuid.UUID, error) {
	id, err := subjectID(r)
	if err != nil {
		return id, err
	}
	if u := auth.UserFrom(r.Context()); u != nil && u.ID == id {
		return id, errOwnHistory
	}
	var ok bool
	if err := h.DB.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM users WHERE id = $1 AND NOT is_bot)`, id).Scan(&ok); err != nil {
		return id, fmt.Errorf("load member: %w", err)
	}
	if !ok {
		return id, httpx.ErrNotFound("member not found")
	}
	return id, nil
}

// auditEvents reads the timeline entries from the audit log. Only a fixed set
// of actions is read, and of their details only the reason and the length.
func (h *Handler) auditEvents(ctx context.Context, id uuid.UUID) ([]HistoryEvent, error) {
	actions := make([]string, 0, len(auditKinds))
	for a := range auditKinds {
		actions = append(actions, a)
	}
	// Loaded before the query so a failure cannot leave its rows open.
	snap, err := h.Perms.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := h.DB.Query(ctx, `
		SELECT created_at, action, actor_name,
		       COALESCE(details #>> '{request,reason}', details ->> 'reason', ''),
		       COALESCE(details #>> '{request,duration_seconds}', details ->> 'duration_seconds', '0'),
		       details #> '{before,role_ids}', details #> '{after,role_ids}'
		FROM audit_log WHERE target_type = 'user' AND target_id = $1 AND action = ANY($2)
		ORDER BY id DESC LIMIT $3`, id.String(), actions, MaxHistoryEvents)
	if err != nil {
		return nil, fmt.Errorf("load moderation history: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (HistoryEvent, error) {
		var ev HistoryEvent
		var action string
		var seconds string
		var before, after []byte
		if err := row.Scan(&ev.At, &action, &ev.Actor, &ev.Reason, &seconds, &before, &after); err != nil {
			return ev, err
		}
		ev.Kind = auditKinds[action]
		_, _ = fmt.Sscan(seconds, &ev.DurationSeconds)
		if ev.Kind == "roles" {
			ev.AddedRoles, ev.RemovedRoles = roleChange(snap, before, after)
		}
		return ev, nil
	})
}

// roleChange names the roles in after but not before, and the other way round.
func roleChange(snap *perm.Snapshot, before, after []byte) (added, removed []string) {
	var b, a []uuid.UUID
	_ = json.Unmarshal(before, &b)
	_ = json.Unmarshal(after, &a)
	name := func(id uuid.UUID) string {
		if r, ok := snap.Role(id); ok {
			return r.Name
		}
		return "(deleted role)"
	}
	for _, id := range a {
		if !slices.Contains(b, id) {
			added = append(added, name(id))
		}
	}
	for _, id := range b {
		if !slices.Contains(a, id) {
			removed = append(removed, name(id))
		}
	}
	return added, removed
}

// reportEvents lists the reports about the member and the automod hits.
func (h *Handler) reportEvents(ctx context.Context, id uuid.UUID) ([]HistoryEvent, error) {
	rows, err := h.DB.Query(ctx, `
		SELECT id, source, category, status, created_at, automod_rule, automod_outcome, hit_count
		FROM message_reports WHERE reported_user_id = $1 ORDER BY created_at DESC, id DESC LIMIT $2`, id, MaxHistoryEvents)
	if err != nil {
		return nil, fmt.Errorf("load reports of member: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (HistoryEvent, error) {
		var ev HistoryEvent
		var rid uuid.UUID
		var source string
		if err := row.Scan(&rid, &source, &ev.Category, &ev.Status, &ev.At, &ev.Rule, &ev.Outcome, &ev.Hits); err != nil {
			return ev, err
		}
		ev.ReportID = &rid
		ev.Kind = "report"
		if source == "automod" {
			ev.Kind = "automod"
		} else {
			ev.Rule, ev.Outcome, ev.Hits = "", "", 0
		}
		return ev, nil
	})
}
