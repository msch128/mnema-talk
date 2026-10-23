package chat

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

func (h *Handler) mountReports(r chi.Router) {
	filing := r
	if h.ReportLimit != nil {
		filing = r.With(h.ReportLimit.By(auth.UserKey))
	}
	filing.Post("/messages/{messageID}/report", httpx.Handle(h.reportMessage))
}

// mountReportsAdmin registers the moderation queue (Manage reports applies).
func (h *Handler) mountReportsAdmin(r chi.Router) {
	r = r.With(auth.RequirePerm(h.Perms, perm.ManageReports))
	r.Get("/reports", httpx.Handle(h.listReports))
	r.Get("/reports/{id}", httpx.Handle(h.getReport))
	r.Post("/reports/{id}/resolve", httpx.Handle(h.resolveReport))
	r.Delete("/reports/{id}", httpx.Handle(h.deleteReport))
}

// ReportFiled is the answer to a filed report. It confirms receipt only.
type ReportFiled struct {
	ID uuid.UUID `json:"id" format:"uuid"`
}

// reportMessage handles POST /api/messages/{messageID}/report.
//
// @Summary Report a message
// @Description Reports a message of a channel or a direct message to the moderators. The caller must be able to see the message (otherwise 404, as for any message they cannot see). The report keeps a copy of the message and up to 20 messages around it, so it still stands after the author deletes the message; it is the only way a moderator sees any part of a direct message. A member cannot report their own message. Repeating a report of the same message while the first is open returns the first. At most 10 reports per hour and 50 open reports per member. Records the audit entry report.create (without message text).
// @ID reportMessage
// @Tags Messages
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param messageID path string true "Message ID." Format(uuid)
// @Param request body CreateReportRequest true "Request body."
// @Success 201 {object} ReportFiled "The report was filed."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT: too many open reports."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/messages/{messageID}/report [post]
func (h *Handler) reportMessage(w http.ResponseWriter, r *http.Request) error {
	msgID, err := httpx.PathUUID(r, "messageID")
	if err != nil {
		return err
	}
	ref, err := loadMessageRef(r.Context(), h.DB, msgID)
	if err != nil {
		return err
	}
	if err := h.require(r, ref.ChannelID, perm.ViewChannel); err != nil {
		return notFoundUnlessFailure(err)
	}
	var req CreateReportRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	note, err := httpx.CleanText("note", req.Note, MaxReportNote, false)
	if err != nil {
		return err
	}
	user := auth.UserFrom(r.Context())
	id, err := FileReport(r.Context(), h.DB, NewReport{Reporter: user.ID, MessageID: msgID, Category: req.Category, Note: note})
	if err != nil {
		return err
	}
	h.auditReport(r.Context(), id, req.Category)
	httpx.WriteJSON(w, http.StatusCreated, ReportFiled{ID: id})
	return nil
}

// auditReport records that a report was filed, never what it contains: the
// entry would otherwise carry direct message text to every reader of the log.
func (h *Handler) auditReport(ctx context.Context, id uuid.UUID, category string) {
	if h.Audit == nil {
		return
	}
	h.Audit.RecordOrLog(ctx, audit.Entry{
		Action: "report.create", TargetType: "report", TargetID: id.String(),
		Details: map[string]any{"category": category},
	})
}

// ReportParty names a member in the queue.
type ReportParty struct {
	ID       uuid.UUID `json:"id" format:"uuid"`
	Username string    `json:"username"`
}

// Report is one entry of the moderation queue.
type Report struct {
	ID       uuid.UUID `json:"id" format:"uuid"`
	Kind     string    `json:"kind" enums:"channel,dm"`
	Category string    `json:"category" enums:"spam,harassment,illegal,other"`
	Status   string    `json:"status" enums:"open,resolved,dismissed"`
	// Note is what the reporter wrote.
	Note      string    `json:"note"`
	MessageID uuid.UUID `json:"message_id" format:"uuid"`
	// ChannelID and ChannelName are left out for a direct message.
	ChannelID   *uuid.UUID   `json:"channel_id,omitempty" binding:"optional" format:"uuid"`
	ChannelName string       `json:"channel_name,omitempty" binding:"optional"`
	Reporter    *ReportParty `json:"reporter,omitempty" binding:"optional"`
	Reported    *ReportParty `json:"reported,omitempty" binding:"optional"`
	CreatedAt   time.Time    `json:"created_at" format:"date-time"`
	ResolvedAt  *time.Time   `json:"resolved_at,omitempty" binding:"optional" format:"date-time"`
	ResolvedBy  *ReportParty `json:"resolved_by,omitempty" binding:"optional"`
	// ResolutionNote is the moderator's note on closing the report.
	ResolutionNote string `json:"resolution_note,omitempty" binding:"optional"`
}

// ReportDetail is a report with the copy of the message and its context.
type ReportDetail struct {
	Report
	Snapshot ReportSnapshot `json:"snapshot"`
}

// ReportsPage is one page of the queue.
type ReportsPage struct {
	Reports []Report `json:"reports"`
	HasMore bool     `json:"has_more"`
}

const reportSelect = `
	SELECT r.id, r.kind, r.category, r.status, r.note, r.message_id, r.channel_id, r.channel_name,
	       r.reporter_id, rp.username, r.reported_user_id, ru.username, r.created_at,
	       r.resolved_at, r.resolved_by, rb.username, r.resolution_note`

const reportFrom = `
	FROM message_reports r
	LEFT JOIN users rp ON rp.id = r.reporter_id
	LEFT JOIN users ru ON ru.id = r.reported_user_id
	LEFT JOIN users rb ON rb.id = r.resolved_by`

func party(id *uuid.UUID, name *string) *ReportParty {
	if id == nil || name == nil {
		return nil
	}
	return &ReportParty{ID: *id, Username: *name}
}

func scanReport(row pgx.CollectableRow) (Report, error) { return scanReportWith(row) }

// scanReportWith scans a reportSelect row followed by the extra columns.
func scanReportWith(row pgx.CollectableRow, extra ...any) (Report, error) {
	var rep Report
	var channel uuid.UUID
	var reporterID, reportedID, resolverID *uuid.UUID
	var reporter, reported, resolver *string
	dest := append([]any{&rep.ID, &rep.Kind, &rep.Category, &rep.Status, &rep.Note, &rep.MessageID, &channel, &rep.ChannelName,
		&reporterID, &reporter, &reportedID, &reported, &rep.CreatedAt, &rep.ResolvedAt, &resolverID, &resolver, &rep.ResolutionNote}, extra...)
	err := row.Scan(dest...)
	if rep.Kind == ReportKindChannel {
		rep.ChannelID = &channel
	}
	rep.Reporter, rep.Reported, rep.ResolvedBy = party(reporterID, reporter), party(reportedID, reported), party(resolverID, resolver)
	return rep, err
}

// listReports handles GET /api/admin/reports.
//
// @Summary List reports
// @Description The moderation queue, newest first. Needs Manage reports (the Owner and Administrators always). The list holds no message text; open a report for that.
// @ID listReports
// @Tags Moderation
// @Produce json
// @Security cookieAuth
// @Param status query string false "Only reports with this status; all lists every status." Enums(open,resolved,dismissed,all) default(open)
// @Param before query string false "Page back: only reports older than this report." Format(uuid)
// @Param limit query int false "Page size, at most 100." minimum(1) maximum(100) default(25)
// @Success 200 {object} ReportsPage "One page of reports."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Manage reports permission."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/reports [get]
func (h *Handler) listReports(w http.ResponseWriter, r *http.Request) error {
	status := r.URL.Query().Get("status")
	switch status {
	case "":
		status = ReportOpen
	case ReportOpen, ReportResolved, ReportDismissed, "all":
	default:
		return httpx.ErrInvalidInput("status must be open, resolved, dismissed or all")
	}
	var before *uuid.UUID
	if _, err := queryIDs(r, "an id", map[string]**uuid.UUID{"before": &before}); err != nil {
		return err
	}
	limit := httpx.QueryLimit(r, "limit", 25, 100)
	rows, err := h.DB.Query(r.Context(), reportSelect+reportFrom+`
		WHERE ($1 = 'all' OR r.status = $1)
		  AND ($2::uuid IS NULL OR (r.created_at, r.id) < (SELECT created_at, id FROM message_reports WHERE id = $2))
		ORDER BY r.created_at DESC, r.id DESC LIMIT $3`, status, before, limit+1)
	if err != nil {
		return fmt.Errorf("list reports: %w", err)
	}
	reports, err := pgx.CollectRows(rows, scanReport)
	if err != nil {
		return fmt.Errorf("list reports: %w", err)
	}
	more := len(reports) > limit
	if more {
		reports = reports[:limit]
	}
	if reports == nil {
		reports = []Report{}
	}
	httpx.WriteJSON(w, http.StatusOK, ReportsPage{Reports: reports, HasMore: more})
	return nil
}

// getReport handles GET /api/admin/reports/{id}.
//
// @Summary Get a report
// @Description The report with the copy of the reported message and up to 20 messages around it, as they were when it was filed. Needs Manage reports. This is the only way a moderator reads any part of a direct message.
// @ID getReport
// @Tags Moderation
// @Produce json
// @Security cookieAuth
// @Param id path string true "Report ID." Format(uuid)
// @Success 200 {object} ReportDetail "The report."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Manage reports permission."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/reports/{id} [get]
func (h *Handler) getReport(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	rows, err := h.DB.Query(r.Context(), reportSelect+`, r.snapshot`+reportFrom+` WHERE r.id = $1`, id)
	if err != nil {
		return fmt.Errorf("get report: %w", err)
	}
	detail, err := pgx.CollectExactlyOneRow(rows, func(row pgx.CollectableRow) (ReportDetail, error) {
		var snap ReportSnapshot
		rep, err := scanReportWith(row, &snap)
		return ReportDetail{Report: rep, Snapshot: snap}, err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return errReportNotFound
	}
	if err != nil {
		return fmt.Errorf("get report: %w", err)
	}
	httpx.WriteJSON(w, http.StatusOK, detail)
	return nil
}

// ResolveReportRequest is the body of POST /api/admin/reports/{id}/resolve.
type ResolveReportRequest struct {
	// Status closes the report: resolved (acted on) or dismissed (no action).
	Status string `json:"status" enums:"resolved,dismissed"`
	// Note is kept with the report, up to 500 characters.
	Note string `json:"note" maxLength:"500" binding:"optional"`
}

// resolveReport handles POST /api/admin/reports/{id}/resolve.
//
// @Summary Close a report
// @Description Marks an open report resolved or dismissed. The reporter is not notified. Needs Manage reports. Recorded in the audit log as report.resolve.
// @ID resolveReport
// @Tags Moderation
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param id path string true "Report ID." Format(uuid)
// @Param request body ResolveReportRequest true "Request body."
// @Success 200 {object} Report "The closed report."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Manage reports permission."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT: the report is already closed."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/reports/{id}/resolve [post]
func (h *Handler) resolveReport(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	var req ResolveReportRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if req.Status != ReportResolved && req.Status != ReportDismissed {
		return httpx.ErrInvalidInput("status must be resolved or dismissed")
	}
	note, err := httpx.CleanText("note", req.Note, MaxReportNote, false)
	if err != nil {
		return err
	}
	tag, err := h.DB.Exec(r.Context(), `
		UPDATE message_reports SET status = $2, resolution_note = $3, resolved_by = $4, resolved_at = NOW()
		WHERE id = $1 AND status = 'open'`, id, req.Status, note, auth.UserFrom(r.Context()).ID)
	if err != nil {
		return fmt.Errorf("resolve report: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return h.reportClosedOrMissing(r.Context(), id)
	}
	rows, err := h.DB.Query(r.Context(), reportSelect+reportFrom+` WHERE r.id = $1`, id)
	if err != nil {
		return fmt.Errorf("resolve report: %w", err)
	}
	rep, err := pgx.CollectExactlyOneRow(rows, scanReport)
	if err != nil {
		return fmt.Errorf("resolve report: %w", err)
	}
	httpx.WriteJSON(w, http.StatusOK, rep)
	return nil
}

func (h *Handler) reportClosedOrMissing(ctx context.Context, id uuid.UUID) error {
	var exists bool
	if err := h.DB.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM message_reports WHERE id = $1)`, id).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return errReportNotFound
	}
	return httpx.ErrConflict("the report is already closed")
}

// deleteReport handles DELETE /api/admin/reports/{id}.
//
// @Summary Delete a report
// @Description Removes a closed report and the copy of the messages it kept. An open report cannot be deleted; close it first. Needs Manage reports. Recorded in the audit log as report.delete.
// @ID deleteReport
// @Tags Moderation
// @Security cookieAuth
// @Param id path string true "Report ID." Format(uuid)
// @Success 204 "Deleted."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Manage reports permission."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT: the report is still open."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/reports/{id} [delete]
func (h *Handler) deleteReport(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	tag, err := h.DB.Exec(r.Context(), `DELETE FROM message_reports WHERE id = $1 AND status <> 'open'`, id)
	if err != nil {
		return fmt.Errorf("delete report: %w", err)
	}
	if tag.RowsAffected() == 0 {
		var exists bool
		if err := h.DB.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM message_reports WHERE id = $1)`, id).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return errReportNotFound
		}
		return httpx.ErrConflict("close the report before deleting it")
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
