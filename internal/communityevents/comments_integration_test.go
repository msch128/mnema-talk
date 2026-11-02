//go:build integration

package communityevents

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

type fakeCommentPusher struct {
	mu   sync.Mutex
	sent []string
}

func (f *fakeCommentPusher) SendEventCommentNotice(_ context.Context, userID, _ uuid.UUID, _, author, excerpt string, _ time.Time, _ *uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, fmt.Sprintf("%s:%s:%s", userID, author, excerpt))
	return nil
}

func (f *fakeCommentPusher) got() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.sent...)
}

func (f *rosterFixture) comment(t *testing.T, u *auth.User, text string) EventComment {
	t.Helper()
	rec, err := f.call(t, u, f.h.createComment, http.MethodPost, fmt.Sprintf(`{"content":%q}`, text), nil)
	if err != nil {
		t.Fatal(err)
	}
	var c EventComment
	if err := json.Unmarshal(rec.Body.Bytes(), &c); err != nil {
		t.Fatal(err)
	}
	return c
}

func (f *rosterFixture) comments(t *testing.T, u *auth.User, query string) CommentList {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/?"+query, nil)
	rc := chi.NewRouteContext()
	rc.URLParams.Add("id", f.event.ID.String())
	req = req.WithContext(context.WithValue(auth.WithUser(req.Context(), u), chi.RouteCtxKey, rc))
	rec := httptest.NewRecorder()
	if err := f.h.listComments(rec, req); err != nil {
		t.Fatal(err)
	}
	var list CommentList
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	return list
}

func TestCommentsPageOldestFirstAndRefuseTheWrongInput(t *testing.T) {
	f := newRosterFixture(t)
	for i := 1; i <= 5; i++ {
		f.comment(t, f.creator, fmt.Sprintf("c%d", i))
	}
	newest := f.comments(t, f.owner, "limit=2")
	if len(newest.Comments) != 2 || newest.Comments[0].Content != "c4" || newest.Comments[1].Content != "c5" || !newest.HasMore {
		t.Fatalf("newest page: %+v", newest)
	}
	older := f.comments(t, f.owner, "limit=2&before="+newest.Comments[0].ID.String())
	if len(older.Comments) != 2 || older.Comments[0].Content != "c2" || !older.HasMore {
		t.Fatalf("older page: %+v", older)
	}
	last := f.comments(t, f.owner, "limit=2&before="+older.Comments[0].ID.String())
	if len(last.Comments) != 1 || last.Comments[0].Content != "c1" || last.HasMore {
		t.Fatalf("last page: %+v", last)
	}

	for name, body := range map[string]string{"blank": `{"content":"   "}`, "too long": fmt.Sprintf(`{"content":%q}`, strings.Repeat("x", MaxCommentLen+1))} {
		if _, err := f.call(t, f.creator, f.h.createComment, http.MethodPost, body, nil); err == nil {
			t.Errorf("%s comment must be refused", name)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/?before=nope", nil)
	rc := chi.NewRouteContext()
	rc.URLParams.Add("id", f.event.ID.String())
	req = req.WithContext(context.WithValue(auth.WithUser(req.Context(), f.owner), chi.RouteCtxKey, rc))
	if err := f.h.listComments(httptest.NewRecorder(), req); err == nil {
		t.Fatal("a malformed cursor is refused")
	}
	if _, err := f.s.ListComments(context.Background(), f.event.ID, ptr(uuid.New()), 10, f.owner.ID); err == nil {
		t.Fatal("an unknown cursor is not found")
	}
}

func TestOnlyTheAuthorEditsAndModeratorsDelete(t *testing.T) {
	f := newRosterFixture(t)
	stranger := f.user(t, "stranger", "user")
	c := f.comment(t, f.creator, "first")
	params := map[string]string{"commentID": c.ID.String()}

	rec, err := f.call(t, f.creator, f.h.updateComment, http.MethodPatch, `{"content":"edited"}`, params)
	if err != nil {
		t.Fatal(err)
	}
	var edited EventComment
	if err := json.Unmarshal(rec.Body.Bytes(), &edited); err != nil || edited.Content != "edited" || edited.EditedAt == nil {
		t.Fatalf("edit: %+v %v", edited, err)
	}
	if _, err := f.call(t, stranger, f.h.updateComment, http.MethodPatch, `{"content":"mine now"}`, params); err == nil {
		t.Fatal("nobody edits another member's comment, not even a moderator")
	}
	if _, err := f.call(t, f.owner, f.h.updateComment, http.MethodPatch, `{"content":"mod edit"}`, params); err == nil {
		t.Fatal("not even the owner edits another member's words")
	}
	if _, err := f.call(t, f.creator, f.h.updateComment, http.MethodPatch, `{"content":""}`, params); err == nil {
		t.Fatal("an empty edit is refused")
	}
	if _, err := f.call(t, f.creator, f.h.updateComment, http.MethodPatch, `{"content":"x"}`, map[string]string{"commentID": uuid.NewString()}); err == nil {
		t.Fatal("an unknown comment is not found")
	}

	if _, err := f.call(t, stranger, f.h.deleteComment, http.MethodDelete, "", params); err == nil {
		t.Fatal("a stranger must not delete it")
	}
	if _, err := f.call(t, f.owner, f.h.deleteComment, http.MethodDelete, "", params); err != nil {
		t.Fatalf("a moderator deletes: %v", err)
	}
	var audited int
	if err := f.p.QueryRow(context.Background(), `SELECT COUNT(*) FROM audit_log WHERE action = 'event.comment_delete'`).Scan(&audited); err != nil || audited != 1 {
		t.Fatalf("moderator deletion audited %d times: %v", audited, err)
	}
	own := f.comment(t, stranger, "mine")
	if _, err := f.call(t, stranger, f.h.deleteComment, http.MethodDelete, "", map[string]string{"commentID": own.ID.String()}); err != nil {
		t.Fatalf("the author deletes: %v", err)
	}
	if err := f.p.QueryRow(context.Background(), `SELECT COUNT(*) FROM audit_log WHERE action = 'event.comment_delete'`).Scan(&audited); err != nil || audited != 1 {
		t.Fatalf("an author's own deletion is not a moderation action: %d", audited)
	}
	if _, err := f.call(t, stranger, f.h.deleteComment, http.MethodDelete, "", map[string]string{"commentID": own.ID.String()}); err == nil {
		t.Fatal("deleting twice is not found")
	}
	if _, err := f.call(t, stranger, f.h.deleteComment, http.MethodDelete, "", map[string]string{"commentID": "nope"}); err == nil {
		t.Fatal("a malformed id is refused")
	}
}

func TestAnEventKeepsAtMostFiveHundredComments(t *testing.T) {
	f := newRosterFixture(t)
	if _, err := f.p.Exec(context.Background(), `
		INSERT INTO event_comments (event_id, user_id, content) SELECT $1, $2, 'c' || g FROM generate_series(1, $3) g`, f.event.ID, f.creator.ID, MaxEventComments); err != nil {
		t.Fatal(err)
	}
	_, err := f.call(t, f.creator, f.h.createComment, http.MethodPost, `{"content":"one more"}`, nil)
	if apiErr, ok := httpx.AsAPIError(err); !ok || apiErr.Status != http.StatusConflict {
		t.Fatalf("the 501st comment is a conflict, got %v", err)
	}
	if _, err := f.s.CreateComment(context.Background(), uuid.New(), f.creator.ID, "x"); err == nil {
		t.Fatal("an unknown event is not found")
	}
}

func TestACommentTellsTheCreatorAndAttendeesButNotTheAuthorOrAMutedMember(t *testing.T) {
	f := newRosterFixture(t)
	cp := &fakeCommentPusher{}
	f.h.commentsPusher = cp
	author := f.user(t, "author", "user")
	goer, interested, notGoing, muted, signedUp := f.user(t, "goer", "user"), f.user(t, "interested", "user"), f.user(t, "notgoing", "user"), f.user(t, "muted", "user"), f.user(t, "signedup", "user")
	for u, resp := range map[*auth.User]string{goer: RSVPGoing, interested: RSVPInterested, notGoing: RSVPNotGoing, muted: RSVPGoing, author: RSVPGoing} {
		if err := f.s.SetRSVP(context.Background(), f.event.ID, u.ID, resp); err != nil {
			t.Fatal(err)
		}
	}
	roles := f.setRoles(t, role("Tank", 1))
	f.signUp(t, signedUp, roles[0].ID)
	if _, err := f.call(t, muted, f.h.muteComments, http.MethodPut, `{"muted":true}`, nil); err != nil {
		t.Fatal(err)
	}
	if got := f.comments(t, muted, ""); !got.Muted {
		t.Fatal("the list says the caller muted the event")
	}

	f.comment(t, author, "See you   at\nseven")
	f.h.WaitNotices()
	told := map[uuid.UUID]bool{}
	for _, e := range f.rec.Snapshot() {
		if e.Type != events.EventCommentNotice {
			continue
		}
		p := e.Payload.(CommentNoticePayload)
		if p.Excerpt != "See you at seven" || p.AuthorName != "author" {
			t.Fatalf("notice: %+v", p)
		}
		told[e.Recipients[0]] = true
	}
	want := map[uuid.UUID]bool{f.creator.ID: true, goer.ID: true, interested.ID: true, signedUp.ID: true}
	if len(told) != len(want) {
		t.Fatalf("told %d members, want %d: %v", len(told), len(want), told)
	}
	for id := range want {
		if !told[id] {
			t.Fatalf("%s was not told", id)
		}
	}
	if len(cp.got()) != len(want) {
		t.Fatalf("pushes: %v", cp.got())
	}
	if _, err := f.call(t, muted, f.h.muteComments, http.MethodPut, `{"muted":false}`, nil); err != nil {
		t.Fatal(err)
	}
	if got := f.comments(t, muted, ""); got.Muted {
		t.Fatal("unmuting works")
	}
	var broadcasts int
	for _, e := range f.rec.Snapshot() {
		if p, ok := e.Payload.(CommentPayload); ok && e.Type == events.EventComment && p.Action == CommentCreated && p.Comment != nil {
			broadcasts++
		}
	}
	if broadcasts != 1 {
		t.Fatalf("the comment is announced once, got %d", broadcasts)
	}
}

func TestCommentsOfATalkEventStayWithThoseWhoCanViewTheTalk(t *testing.T) {
	f := newRosterFixture(t)
	talk := f.privateTalk(t)
	stranger := f.user(t, "stranger", "user")
	if _, err := f.call(t, stranger, f.h.createComment, http.MethodPost, `{"content":"hi"}`, nil); err == nil {
		t.Fatal("a member who cannot view the Talk must not comment")
	}
	if _, err := f.call(t, stranger, f.h.muteComments, http.MethodPut, `{"muted":true}`, nil); err == nil {
		t.Fatal("nor mute")
	}
	c := f.comment(t, f.owner, "inside")
	chans := 0
	for _, e := range f.rec.Snapshot() {
		if e.Type == events.EventComment && e.ChannelID == talk {
			chans++
		}
	}
	if chans != 1 {
		t.Fatalf("a Talk event's comment goes to the Talk's viewers only, got %d channel broadcasts", chans)
	}
	// A recipient who lost sight of the Talk is told nothing.
	if err := f.s.SetRSVP(context.Background(), f.event.ID, stranger.ID, RSVPGoing); err != nil {
		t.Fatal(err)
	}
	ev, _ := f.s.Get(context.Background(), f.event.ID, nil)
	f.h.notifyComment(context.Background(), ev, c)
	for _, e := range f.rec.Snapshot() {
		if e.Type == events.EventCommentNotice && len(e.Recipients) == 1 && e.Recipients[0] == stranger.ID {
			t.Fatal("a member who cannot view the Talk is not told")
		}
	}
}

func TestCommentRoutesAreMountedWithTheRateLimit(t *testing.T) {
	f := newRosterFixture(t)
	f.h.CommentLimit = httpx.NewRateLimiter(CommentsPerMinute, time.Minute)
	r := chi.NewRouter()
	f.h.Mount(r)
	for _, route := range [][2]string{
		{http.MethodGet, "/events/x/comments"}, {http.MethodPost, "/events/x/comments"}, {http.MethodPatch, "/events/x/comments/y"},
		{http.MethodDelete, "/events/x/comments/y"}, {http.MethodPut, "/events/x/comments/mute"},
	} {
		if !r.Match(chi.NewRouteContext(), route[0], route[1]) {
			t.Errorf("%s %s is not mounted", route[0], route[1])
		}
	}
	if got := excerptOf(strings.Repeat("a", commentExcerptLen+10)); len([]rune(got)) != commentExcerptLen+1 {
		t.Fatalf("a long excerpt is cut: %d", len([]rune(got)))
	}
	bare := &Handler{DB: f.p, Store: f.s}
	if bare.commentPusher() != nil {
		t.Fatal("without push there is no pusher")
	}
	bare.notifyComment(context.Background(), f.event, EventComment{User: UserRef{ID: uuid.New(), Username: "x"}, Content: "hi"})
}

func TestCommentsNeedSendMessages(t *testing.T) {
	f := newRosterFixture(t)
	ctx := context.Background()
	timedOut, pending := f.user(t, "timedout", "user"), f.user(t, "pending", "user")
	c := f.comment(t, timedOut, "before the timeout")
	params := map[string]string{"commentID": c.ID.String()}
	body := `{"content":"after"}`

	if _, err := f.p.Exec(ctx, `UPDATE users SET timeout_until = NOW() + interval '1 hour' WHERE id = $1`, timedOut.ID); err != nil {
		t.Fatal(err)
	}
	f.h.Perms.Invalidate()
	if _, err := f.call(t, timedOut, f.h.createComment, http.MethodPost, body, nil); err == nil {
		t.Fatal("a member in timeout must not comment")
	}
	if _, err := f.call(t, timedOut, f.h.updateComment, http.MethodPatch, body, params); err == nil {
		t.Fatal("nor edit")
	}
	// Taking back is allowed: delete and mute stay open.
	if _, err := f.call(t, timedOut, f.h.muteComments, http.MethodPut, `{"muted":true}`, nil); err != nil {
		t.Fatalf("mute stays open: %v", err)
	}
	if _, err := f.call(t, timedOut, f.h.deleteComment, http.MethodDelete, "", params); err != nil {
		t.Fatalf("delete stays open: %v", err)
	}

	if _, err := f.p.Exec(ctx, `UPDATE instance_settings SET rules_text = 'Be kind', rules_version = 3`); err != nil {
		t.Fatal(err)
	}
	f.h.Perms.Invalidate()
	_, err := f.call(t, pending, f.h.createComment, http.MethodPost, body, nil)
	if apiErr, ok := httpx.AsAPIError(err); !ok || apiErr.Code != httpx.CodeRulesNotAccepted {
		t.Fatalf("a member who has not accepted the rules is told so, got %v", err)
	}
}

func TestATalkThatDeniesSendMessagesRefusesComments(t *testing.T) {
	f := newRosterFixture(t)
	talk := f.privateTalk(t)
	member := f.user(t, "member", "user")
	var everyone uuid.UUID
	if err := f.p.QueryRow(context.Background(), `SELECT id FROM roles WHERE is_everyone`).Scan(&everyone); err != nil {
		t.Fatal(err)
	}
	// Viewable again, but no writing in this Talk.
	if _, err := f.p.Exec(context.Background(), `DELETE FROM permission_overwrites WHERE channel_id = $1`, talk); err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.Exec(context.Background(), `INSERT INTO permission_overwrites (channel_id, role_id, deny) VALUES ($1, $2, $3)`, talk, everyone, int64(perm.SendMessages)); err != nil {
		t.Fatal(err)
	}
	f.h.Perms.Invalidate()
	if _, err := f.call(t, member, f.h.createComment, http.MethodPost, `{"content":"hi"}`, nil); err == nil {
		t.Fatal("a Talk that denies Send messages takes no comments")
	}
	if got := f.comments(t, member, ""); len(got.Comments) != 0 {
		t.Fatalf("reading stays open: %+v", got)
	}
}

func TestCommentRulesOfTheEvent(t *testing.T) {
	f := newRosterFixture(t)
	ctx := context.Background()
	// One member cannot fill the thread.
	if _, err := f.p.Exec(ctx, `INSERT INTO event_comments (event_id, user_id, content) SELECT $1, $2, 'c' || g FROM generate_series(1, $3) g`, f.event.ID, f.creator.ID, MaxCommentsPerMember); err != nil {
		t.Fatal(err)
	}
	if _, err := f.call(t, f.creator, f.h.createComment, http.MethodPost, `{"content":"one too many"}`, nil); err == nil {
		t.Fatal("a member's share of one event is capped")
	}
	other := f.user(t, "other", "user")
	f.comment(t, other, "others still can")

	// The creator moderates their own event without a permission; a stranger does not.
	c := f.comment(t, other, "to be removed")
	stranger := f.user(t, "stranger", "user")
	if _, err := f.call(t, stranger, f.h.deleteComment, http.MethodDelete, "", map[string]string{"commentID": c.ID.String()}); err == nil {
		t.Fatal("a stranger must not delete it")
	}
	if _, err := f.call(t, f.creator, f.h.deleteComment, http.MethodDelete, "", map[string]string{"commentID": c.ID.String()}); err != nil {
		t.Fatalf("the creator deletes under their event: %v", err)
	}

	// Comments stay open after the event, and close when it is cancelled.
	if _, err := f.p.Exec(ctx, `UPDATE events SET status = 'completed' WHERE id = $1`, f.event.ID); err != nil {
		t.Fatal(err)
	}
	f.comment(t, other, "thanks for the evening")
	if _, err := f.p.Exec(ctx, `UPDATE events SET status = 'cancelled' WHERE id = $1`, f.event.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.call(t, other, f.h.createComment, http.MethodPost, `{"content":"hello?"}`, nil); err == nil {
		t.Fatal("a cancelled event takes no more comments")
	}
}

func TestNoticesSpareBlockersAndThoseWhoDoNotWantToBeDisturbed(t *testing.T) {
	f := newRosterFixture(t)
	ctx := context.Background()
	author, blocker, dnd, first, interested := f.user(t, "author", "user"), f.user(t, "blocker", "user"), f.user(t, "dnd", "user"), f.user(t, "going", "user"), f.user(t, "interested", "user")
	for u, resp := range map[*auth.User]string{blocker: RSVPGoing, dnd: RSVPGoing, first: RSVPGoing, interested: RSVPInterested} {
		if err := f.s.SetRSVP(ctx, f.event.ID, u.ID, resp); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.p.Exec(ctx, `INSERT INTO user_blocks (blocker_id, blocked_id) VALUES ($1, $2)`, blocker.ID, author.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.Exec(ctx, `UPDATE users SET presence = 'dnd' WHERE id = $1`, dnd.ID); err != nil {
		t.Fatal(err)
	}
	cp := &fakeCommentPusher{}
	f.h.commentsPusher = cp

	ids, err := f.s.CommentRecipients(ctx, f.event.ID, author.ID)
	if err != nil || len(ids) != 4 || ids[0].UserID != f.creator.ID || ids[len(ids)-1].UserID != interested.ID {
		t.Fatalf("recipients (creator first, interested last, no blocker): %+v %v", ids, err)
	}
	f.comment(t, author, "hello")
	f.h.WaitNotices()
	toasted := map[uuid.UUID]bool{}
	for _, e := range f.rec.Snapshot() {
		if e.Type == events.EventCommentNotice {
			toasted[e.Recipients[0]] = true
		}
	}
	if toasted[blocker.ID] || toasted[dnd.ID] || !toasted[first.ID] || !toasted[f.creator.ID] {
		t.Fatalf("toasts: %v", toasted)
	}
	for _, s := range cp.got() {
		if strings.HasPrefix(s, blocker.ID.String()) {
			t.Fatal("a blocker gets no push")
		}
	}
}
