package chat

import (
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// The queue's wire types and the one row scan behind its three reads.

// ReportParty names a member in the queue.
type ReportParty struct {
	ID       uuid.UUID `json:"id" format:"uuid"`
	Username string    `json:"username"`
}

// Report is one entry of the moderation queue.
type Report struct {
	ID uuid.UUID `json:"id" format:"uuid"`
	// Kind is channel or dm for a reported message, alert for an automod
	// alert that is about no message (a raid).
	Kind     string `json:"kind" enums:"channel,dm,alert"`
	Category string `json:"category" enums:"spam,harassment,illegal,other,automod"`
	Status   string `json:"status" enums:"open,resolved,dismissed"`
	// Note is what the reporter wrote.
	Note string `json:"note"`
	// MessageID is left out for an automod entry about a blocked message
	// (it was never stored) and for an alert.
	MessageID *uuid.UUID `json:"message_id,omitempty" binding:"optional" format:"uuid"`
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
	// Source is member for a report a member filed, automod for an entry the
	// automod rules made (no Reporter then).
	Source string `json:"source" binding:"optional" enums:"member,automod"`
	// AutomodRule names the rule of an automod entry, as it was when it hit.
	AutomodRule string `json:"automod_rule,omitempty" binding:"optional"`
	// AutomodOutcome says what the rule did: blocked the message, only
	// flagged it, or raised a raid alert.
	AutomodOutcome string `json:"automod_outcome,omitempty" binding:"optional" enums:"blocked,flagged,alert"`
	// HitCount counts the hits merged into an automod entry (1 for a report).
	HitCount int `json:"hit_count" binding:"optional"`
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
	       r.resolved_at, r.resolved_by, rb.username, r.resolution_note,
	       r.source, r.automod_rule, r.automod_outcome, r.hit_count`

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
	var channel *uuid.UUID
	var reporterID, reportedID, resolverID *uuid.UUID
	var reporter, reported, resolver *string
	dest := append([]any{&rep.ID, &rep.Kind, &rep.Category, &rep.Status, &rep.Note, &rep.MessageID, &channel, &rep.ChannelName,
		&reporterID, &reporter, &reportedID, &reported, &rep.CreatedAt, &rep.ResolvedAt, &resolverID, &resolver, &rep.ResolutionNote,
		&rep.Source, &rep.AutomodRule, &rep.AutomodOutcome, &rep.HitCount}, extra...)
	err := row.Scan(dest...)
	if rep.Kind == ReportKindChannel {
		rep.ChannelID = channel
	}
	rep.Reporter, rep.Reported, rep.ResolvedBy = party(reporterID, reporter), party(reportedID, reported), party(resolverID, resolver)
	return rep, err
}
