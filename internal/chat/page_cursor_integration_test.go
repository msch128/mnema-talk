//go:build integration

package chat

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
)

func requireStatus(t *testing.T, err error, status int) {
	t.Helper()
	var apiErr *httpx.APIError
	if !errors.As(err, &apiErr) || apiErr.Status != status {
		t.Fatalf("error = %v; want API status %d", err, status)
	}
}

// A page cursor whose row was deleted since the previous page is an error, not
// an empty last page that makes the client believe the list has ended.
func TestPageCursorOfADeletedRowIsNotFound(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	gone := uuid.New()

	_, _, err := Search(ctx, f.p, SearchQuery{Text: "hello", Before: &gone, Limit: 5})
	requireStatus(t, err, http.StatusNotFound)
	_, _, err = Mentions(ctx, f.p, InboxQuery{User: f.u.ID, Before: &gone, Limit: 5})
	requireStatus(t, err, http.StatusNotFound)
	_, err = ListSaved(ctx, f.p, f.u.ID, nil, &gone, 5)
	requireStatus(t, err, http.StatusNotFound)
	_, err = ListForumPosts(ctx, f.p, f.c.ID, "newest", nil, HistoryQuery{Limit: 5, Before: &gone})
	requireStatus(t, err, http.StatusNotFound)

	h := &Handler{DB: f.p, Events: &events.Recorder{}}
	err = h.listReports(httptest.NewRecorder(), chatRequest(f.u, "", nil, "before="+gone.String()))
	requireStatus(t, err, http.StatusNotFound)

	// A cursor that exists still pages.
	if _, _, err := Search(ctx, f.p, SearchQuery{Text: "hello", Before: &f.m, Limit: 5}); err != nil {
		t.Fatalf("search with a live cursor: %v", err)
	}
	if _, err := SaveMessage(ctx, f.p, f.u.ID, f.m, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := ListSaved(ctx, f.p, f.u.ID, nil, &f.m, 5); err != nil {
		t.Fatalf("saved list with a live cursor: %v", err)
	}
	// Unsaving the cursor entry makes it gone for the saved list.
	if _, err := f.p.Exec(ctx, `DELETE FROM saved_messages WHERE user_id = $1`, f.u.ID); err != nil {
		t.Fatal(err)
	}
	_, err = ListSaved(ctx, f.p, f.u.ID, nil, &f.m, 5)
	requireStatus(t, err, http.StatusNotFound)
}

// A reply whose thread root is deleted after it was validated is refused as
// invalid input, like any reply to a missing root, not as a server fault.
func TestReplyToARootDeletedMeanwhileIsAnInputError(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	if _, err := f.p.Exec(ctx, `DELETE FROM messages WHERE id = $1`, f.m); err != nil {
		t.Fatal(err)
	}
	_, err := CreateMessage(ctx, f.p, NewMessage{ChannelID: f.c.ID, UserID: f.u.ID, Content: "late reply", ParentID: &f.m})
	requireStatus(t, err, http.StatusBadRequest)
	requireChatError(t, err, "parent message not found")
}

// Closing a report answers with the closed report; closing it again conflicts
// and a deleted one is gone, with the same existence probe behind both.
func TestResolveAndDeleteReportStates(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	var reporter uuid.UUID
	if err := f.p.QueryRow(ctx, `INSERT INTO users (username, display_name, password_hash) VALUES ('reporter_two', 'Reporter', 'x') RETURNING id`).Scan(&reporter); err != nil {
		t.Fatal(err)
	}
	id, err := FileReport(ctx, f.p, NewReport{Reporter: reporter, MessageID: f.m, Category: "spam"})
	if err != nil {
		t.Fatal(err)
	}
	h := &Handler{DB: f.p, Events: &events.Recorder{}}
	params := map[string]string{"id": id.String()}
	resolve := func() (*httptest.ResponseRecorder, error) {
		w := httptest.NewRecorder()
		return w, h.resolveReport(w, chatRequest(f.u, `{"status":"resolved","note":"done"}`, params, ""))
	}
	del := func() error {
		return h.deleteReport(httptest.NewRecorder(), chatRequest(f.u, "", params, ""))
	}

	requireStatus(t, del(), http.StatusConflict)
	w, err := resolve()
	if err != nil || w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"resolved"`) {
		t.Fatalf("resolve: %v %d %s", err, w.Code, w.Body)
	}
	_, err = resolve()
	requireStatus(t, err, http.StatusConflict)
	if err := del(); err != nil {
		t.Fatal(err)
	}
	_, err = resolve()
	requireStatus(t, err, http.StatusNotFound)
	requireStatus(t, del(), http.StatusNotFound)
}
