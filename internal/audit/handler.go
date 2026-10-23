package audit

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/msch128/mnema-talk/internal/httpx"
)

// Item is one audit log entry as the API returns it.
type Item struct {
	ID        int64     `json:"id"`
	CreatedAt time.Time `json:"created_at" format:"date-time"`
	// ActorID is null for entries the server wrote itself.
	ActorID    *uuid.UUID `json:"actor_id" format:"uuid" extensions:"x-nullable"`
	ActorName  string     `json:"actor_name"`
	Action     string     `json:"action" example:"role.update"`
	TargetType string     `json:"target_type" example:"role"`
	TargetID   string     `json:"target_id"`
	TargetName string     `json:"target_name"`
	// Details holds the sanitized request, a before snapshot and the changed
	// fields; its shape depends on the action.
	Details   map[string]any `json:"details"`
	RequestID string         `json:"request_id"`
}

// Page is a page of entries, newest first.
type Page struct {
	Entries []Item `json:"entries"`
	// NextBefore is the value of the before parameter that fetches the next
	// page; null on the last page.
	NextBefore *int64 `json:"next_before" extensions:"x-nullable"`
}

// Mount registers the read API. The caller guards it with the View audit
// log permission.
func (l *Log) Mount(r chi.Router) {
	r.Get("/audit-log", httpx.Handle(l.list))
}

// list handles GET /api/admin/audit-log.
//
// @Summary List audit log entries
// @Description Newest first, filtered and paged by id. Requires the View audit log permission (the Owner always has it). What a reader sees depends on the entry's class (doc/moderation.md): sign-in events, webhooks, system, media, settings and Owner-only account actions are only returned to the Owner and Administrators, and message and Talk entries only for channels the caller can view. Entries are immutable. Secrets are never stored; message deletions and edits keep the message text.
// @ID listAuditLog
// @Tags Admin
// @Produce json
// @Security cookieAuth
// @Param actor query string false "Actor user id." format(uuid)
// @Param action query string false "Exact action (role.update) or a group (role matches every role.*)."
// @Param target_type query string false "Target type, e.g. user, role, channel."
// @Param target_id query string false "Target id."
// @Param from query string false "Only entries at or after this time (RFC 3339 or YYYY-MM-DD)." format(date-time)
// @Param to query string false "Only entries before this time (RFC 3339 or YYYY-MM-DD, end of day)." format(date-time)
// @Param before query integer false "Entries with a smaller id only (the previous page's next_before)."
// @Param limit query integer false "Page size, 1-200." default(50)
// @Success 200 {object} Page "Entries."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed filter."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the View audit log permission."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/audit-log [get]
func (l *Log) list(w http.ResponseWriter, r *http.Request) error {
	var f filter
	if err := f.parse(r.URL.Query()); err != nil {
		return err
	}
	var viewer Viewer
	if l.Viewer != nil {
		var err error
		if viewer, err = l.Viewer(r.Context()); err != nil {
			return err
		}
	}
	f.restrictTo(viewer)
	limit := httpx.QueryLimit(r, "limit", 50, 200)
	page, err := l.page(r.Context(), f, limit)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, page)
	return nil
}

// page reads the newest entries matching the filter, one more than limit to
// learn whether another page follows.
func (l *Log) page(ctx context.Context, f filter, limit int) (Page, error) {
	sql, args := f.query(limit + 1)
	rows, err := l.DB.Query(ctx, sql, args...)
	if err != nil {
		return Page{}, err
	}
	defer rows.Close()
	page := Page{Entries: []Item{}}
	for rows.Next() {
		var it Item
		if err := rows.Scan(&it.ID, &it.CreatedAt, &it.ActorID, &it.ActorName, &it.Action, &it.TargetType, &it.TargetID, &it.TargetName, &it.Details, &it.RequestID); err != nil {
			return Page{}, err
		}
		page.Entries = append(page.Entries, it)
	}
	if err := rows.Err(); err != nil {
		return Page{}, err
	}
	if len(page.Entries) > limit {
		page.Entries = page.Entries[:limit]
		next := page.Entries[limit-1].ID
		page.NextBefore = &next
	}
	return page, nil
}

func parseTime(s string, endOfDay bool) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, err
	}
	if endOfDay {
		t = t.AddDate(0, 0, 1)
	}
	return t, nil
}
