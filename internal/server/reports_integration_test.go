//go:build integration

package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/perm"
)

type reportsFixture struct {
	a         *app
	admin     *client
	reporter  *client
	author    *client
	moderator *client
	channel   uuid.UUID
}

func newReportsFixture(t *testing.T) *reportsFixture {
	t.Helper()
	a := newApp(t, false)
	admin := a.seedAdmin()
	f := &reportsFixture{a: a, admin: admin}
	f.reporter = a.register(admin, "reporter")
	f.author = a.register(admin, "author")
	f.moderator = a.register(admin, "moderator")
	assignRoles(t, admin, f.moderator, createRole(t, admin, "report readers", perm.ManageReports))
	f.channel = a.createChannel(admin, "general", "text")
	return f
}

func (f *reportsFixture) report(c *client, msg uuid.UUID, category, note string) response {
	return c.post("/api/messages/"+msg.String()+"/report", map[string]any{"category": category, "note": note})
}

func (f *reportsFixture) file(msg uuid.UUID) uuid.UUID {
	f.a.t.Helper()
	return f.fileAs(f.reporter, msg, "this is not ok")
}

func (f *reportsFixture) fileAs(c *client, msg uuid.UUID, note string) uuid.UUID {
	f.a.t.Helper()
	res := f.report(c, msg, "harassment", note)
	wantStatus(f.a.t, "report", res, http.StatusCreated)
	var out struct{ ID uuid.UUID }
	res.decode(f.a.t, &out)
	return out.ID
}

type reportDetail struct {
	ID        uuid.UUID `json:"id"`
	Kind      string    `json:"kind"`
	Status    string    `json:"status"`
	Category  string    `json:"category"`
	Note      string    `json:"note"`
	ChannelID *string   `json:"channel_id"`
	Reporter  struct{ Username string }
	Reported  struct{ Username string }
	Snapshot  struct {
		Message struct {
			ID      uuid.UUID `json:"id"`
			Content string    `json:"content"`
		} `json:"message"`
		Context []struct {
			Content string `json:"content"`
		} `json:"context"`
	} `json:"snapshot"`
}

func (f *reportsFixture) detail(c *client, id uuid.UUID) reportDetail {
	f.a.t.Helper()
	res := c.get("/api/admin/reports/" + id.String())
	wantStatus(f.a.t, "report detail", res, http.StatusOK)
	var d reportDetail
	res.decode(f.a.t, &d)
	return d
}

func TestAReportKeepsTheMessageAndItsContextForTheModerators(t *testing.T) {
	f := newReportsFixture(t)
	for i := range 30 {
		f.a.send(f.author, f.channel, fmt.Sprintf("line %02d", i))
	}
	target := f.a.send(f.author, f.channel, "the reported line")
	for i := range 10 {
		f.a.send(f.reporter, f.channel, fmt.Sprintf("later %02d", i))
	}

	id := f.file(target)
	d := f.detail(f.moderator, id)
	if d.Kind != "channel" || d.Status != "open" || d.Category != "harassment" || d.Note != "this is not ok" {
		t.Fatalf("report: %+v", d)
	}
	if d.Reporter.Username != "reporter" || d.Reported.Username != "author" || d.ChannelID == nil {
		t.Fatalf("parties: %+v", d)
	}
	if d.Snapshot.Message.Content != "the reported line" {
		t.Fatalf("message: %+v", d.Snapshot.Message)
	}
	ctx := d.Snapshot.Context
	if len(ctx) != chat.ReportContextBefore+chat.ReportContextAfter {
		t.Fatalf("context has %d messages, want %d", len(ctx), chat.ReportContextBefore+chat.ReportContextAfter)
	}
	if ctx[0].Content != "line 15" || ctx[chat.ReportContextBefore-1].Content != "line 29" || ctx[chat.ReportContextBefore].Content != "later 00" {
		t.Fatalf("context is not the messages around the reported one, oldest first: first %q, last before %q", ctx[0].Content, ctx[chat.ReportContextBefore-1].Content)
	}

	// The author deleting the message does not undo the report.
	wantStatus(t, "delete", f.author.delete("/api/channels/"+f.channel.String()+"/messages/"+target.String()), http.StatusNoContent)
	if again := f.detail(f.moderator, id); again.Snapshot.Message.Content != "the reported line" {
		t.Fatalf("the copy must outlive the message: %+v", again.Snapshot.Message)
	}
}

func TestOnlyTheReportQueuePermissionOpensReports(t *testing.T) {
	f := newReportsFixture(t)
	id := f.file(f.a.send(f.author, f.channel, "rude"))
	path := "/api/admin/reports/" + id.String()

	for name, c := range map[string]*client{"reporter": f.reporter, "author": f.author} {
		wantStatus(t, name+" list", c.get("/api/admin/reports"), http.StatusForbidden)
		wantStatus(t, name+" read", c.get(path), http.StatusForbidden)
		wantStatus(t, name+" resolve", c.post(path+"/resolve", map[string]any{"status": "resolved"}), http.StatusForbidden)
		wantStatus(t, name+" delete", c.delete(path), http.StatusForbidden)
	}
	// Message moderators do not read reports unless they hold the permission.
	messageMod := f.a.register(f.admin, "msgmod")
	assignRoles(t, f.admin, messageMod, createRole(t, f.admin, "message mods", perm.ManageMessages|perm.TimeoutMembers|perm.BanMembers))
	wantStatus(t, "message moderator", messageMod.get("/api/admin/reports"), http.StatusForbidden)

	for name, c := range map[string]*client{"moderator": f.moderator, "owner": f.admin} {
		wantStatus(t, name+" read", c.get(path), http.StatusOK)
	}
	res := f.moderator.get("/api/admin/reports")
	wantStatus(t, "list", res, http.StatusOK)
	if strings.Contains(string(res.body), "rude") {
		t.Errorf("the list must not carry message text: %s", res.body)
	}
}

func TestReportingNeedsToSeeTheMessageAndNeverRevealsOneYouCannot(t *testing.T) {
	f := newReportsFixture(t)
	staff := createRole(t, f.admin, "Staff", 0)
	everyone := everyoneID(t, f.admin)
	insider := f.a.register(f.admin, "insider")
	assignRoles(t, f.admin, insider, staff)
	secret := f.a.createChannel(f.admin, "staff-only", "text")
	wantStatus(t, "private", putOverwrites(f.admin, "channels", secret,
		overwrite("role", everyone, 0, perm.ViewChannel), overwrite("role", staff, perm.ViewChannel, 0)), http.StatusNoContent)
	hidden := f.a.send(insider, secret, "staff secret")

	// An outsider gets the answer a missing message gets.
	missing := f.report(f.reporter, uuid.New(), "spam", "")
	hiddenRes := f.report(f.reporter, hidden, "spam", "")
	if missing.status != http.StatusNotFound || hiddenRes.status != http.StatusNotFound || string(missing.body) == "" {
		t.Fatalf("missing %d, hidden %d", missing.status, hiddenRes.status)
	}
	if got := hiddenRes.code(t); got != missing.code(t) {
		t.Errorf("hidden message answers %q, missing answers %q", got, missing.code(t))
	}

	visible := f.a.send(f.author, f.channel, "mine")
	wantStatus(t, "own message", f.report(f.author, visible, "spam", ""), http.StatusBadRequest)
	wantStatus(t, "unknown category", f.report(f.reporter, visible, "rude", ""), http.StatusBadRequest)
	wantStatus(t, "long note", f.report(f.reporter, visible, "spam", strings.Repeat("x", chat.MaxReportNote+1)), http.StatusBadRequest)
	wantStatus(t, "signed out", f.report(f.a.anon(), visible, "spam", ""), http.StatusUnauthorized)
}

func TestReportsAreDeduplicatedClosedOnceAndDeletedOnlyWhenClosed(t *testing.T) {
	f := newReportsFixture(t)
	msg := f.a.send(f.author, f.channel, "twice")
	first := f.file(msg)
	if second := f.file(msg); second != first {
		t.Fatalf("repeating an open report must return the first: %s vs %s", second, first)
	}
	path := "/api/admin/reports/" + first.String()
	wantStatus(t, "delete open", f.moderator.delete(path), http.StatusConflict)
	wantStatus(t, "bad status", f.moderator.post(path+"/resolve", map[string]any{"status": "open"}), http.StatusBadRequest)
	wantStatus(t, "resolve", f.moderator.post(path+"/resolve", map[string]any{"status": "dismissed", "note": "fine"}), http.StatusOK)
	wantStatus(t, "resolve twice", f.moderator.post(path+"/resolve", map[string]any{"status": "resolved"}), http.StatusConflict)
	wantStatus(t, "resolve unknown", f.moderator.post("/api/admin/reports/"+uuid.NewString()+"/resolve", map[string]any{"status": "resolved"}), http.StatusNotFound)

	if d := f.detail(f.moderator, first); d.Status != "dismissed" {
		t.Fatalf("status %q", d.Status)
	}
	var open struct{ Reports []reportDetail }
	f.moderator.get("/api/admin/reports").decode(t, &open)
	if len(open.Reports) != 0 {
		t.Errorf("a closed report leaves the open queue: %+v", open.Reports)
	}
	wantStatus(t, "delete closed", f.moderator.delete(path), http.StatusNoContent)
	wantStatus(t, "gone", f.moderator.get(path), http.StatusNotFound)
	// Once closed, the same message can be reported again.
	f.file(msg)
}

func TestFilingReportsIsLimited(t *testing.T) {
	f := newReportsFixture(t)
	for i := range chat.MaxOpenReportsPerMember {
		_, err := f.a.db.Exec(context.Background(), `
			INSERT INTO message_reports (reporter_id, message_id, channel_id, kind, category, snapshot)
			VALUES ($1, gen_random_uuid(), $2, 'channel', 'spam', '{}')`, f.reporter.user.ID, f.channel)
		if err != nil {
			t.Fatal(i, err)
		}
	}
	wantStatus(t, "open limit", f.report(f.reporter, f.a.send(f.author, f.channel, "one more"), "spam", ""), http.StatusConflict)
}

func TestReportAuditEntriesNeverCarryMessageText(t *testing.T) {
	f := newReportsFixture(t)
	id := f.file(f.a.send(f.author, f.channel, "highly sensitive words"))
	wantStatus(t, "resolve", f.moderator.post("/api/admin/reports/"+id.String()+"/resolve", map[string]any{"status": "resolved"}), http.StatusOK)

	res := f.admin.get("/api/admin/audit-log?limit=100")
	wantStatus(t, "audit", res, http.StatusOK)
	body := string(res.body)
	for _, action := range []string{"report.create", "report.resolve"} {
		if !strings.Contains(body, action) {
			t.Errorf("audit log lacks %s: %s", action, body)
		}
	}
	if strings.Contains(body, "highly sensitive words") || strings.Contains(body, "this is not ok") {
		t.Errorf("an audit entry carries reported text: %s", body)
	}
	// Only the Owner and Administrators read them.
	auditor := f.a.register(f.admin, "auditor")
	assignRoles(t, f.admin, auditor, createRole(t, f.admin, "auditors", perm.ViewAuditLog))
	if body := string(auditor.get("/api/admin/audit-log?limit=100").body); strings.Contains(body, "report.") {
		t.Errorf("auditors must not see report entries: %s", body)
	}
}

func TestErasingAnAccountBlanksItsTextInReportCopies(t *testing.T) {
	f := newReportsFixture(t)
	f.a.send(f.author, f.channel, "context by the author")
	reported := f.a.send(f.reporter, f.channel, "reported text by the reporter")
	f.a.send(f.author, f.channel, "after, by the author")
	id := f.fileAs(f.author, reported, "reporter is the author who will be erased")

	wantStatus(t, "erase", f.a.deleteAccount(f.author, "member-password-123", ""), http.StatusNoContent)
	d := f.detail(f.moderator, id)
	if d.Snapshot.Message.Content != "reported text by the reporter" {
		t.Errorf("the other person's text stays: %+v", d.Snapshot.Message)
	}
	for _, m := range d.Snapshot.Context {
		if strings.Contains(m.Content, "author") {
			t.Errorf("the erased account's text survived in a copy: %q", m.Content)
		}
	}
	if d.Reporter.Username != "" && strings.Contains(d.Reporter.Username, "author") {
		t.Errorf("reporter %+v", d.Reporter)
	}
}
