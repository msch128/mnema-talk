//go:build integration

package chat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// When the permission state cannot be loaded every guarded handler fails
// (closed) instead of letting the request through.
func TestHandlersFailClosedWithoutPermissionState(t *testing.T) {
	f := newChatFixture(t)
	cat, err := CreateCategory(context.Background(), f.p, "area", 0)
	if err != nil {
		t.Fatal(err)
	}
	var number int64
	if err := f.p.QueryRow(context.Background(), `SELECT number FROM messages WHERE id = $1`, f.m).Scan(&number); err != nil {
		t.Fatal(err)
	}
	h := &Handler{DB: f.p, Events: &events.Recorder{}, Perms: perm.NewService(nil)}
	ch, msg := f.c.ID.String(), f.m.String()
	params := map[string]string{"channelID": ch, "messageID": msg, "id": ch}
	catParams := map[string]string{"id": cat.ID.String()}

	for _, c := range []struct {
		name   string
		run    func(*http.Request) error
		body   string
		params map[string]string
		query  string
	}{
		{"members", func(r *http.Request) error { return h.listMembers(httptest.NewRecorder(), r) }, "", nil, ""},
		{"channels", func(r *http.Request) error { return h.listChannels(httptest.NewRecorder(), r) }, "", nil, ""},
		{"history", func(r *http.Request) error { return h.listMessages(httptest.NewRecorder(), r) }, "", params, ""},
		{"post", func(r *http.Request) error { return h.createMessage(httptest.NewRecorder(), r) }, `{"content":"x"}`, params, ""},
		{"edit", func(r *http.Request) error { return h.editMessage(httptest.NewRecorder(), r) }, `{"content":"x"}`, params, ""},
		{"delete", func(r *http.Request) error { return h.deleteMessage(httptest.NewRecorder(), r) }, "", params, ""},
		{"react", func(r *http.Request) error { return h.toggleReaction(httptest.NewRecorder(), r) }, `{"emoji":"x"}`, params, ""},
		{"thread", func(r *http.Request) error { return h.getThread(httptest.NewRecorder(), r) }, "", params, ""},
		{"by number", func(r *http.Request) error { return h.getMessageByNumber(httptest.NewRecorder(), r) }, "", map[string]string{"number": strconv.FormatInt(number, 10)}, ""},
		{"read state", func(r *http.Request) error { return h.readState(httptest.NewRecorder(), r) }, "", nil, ""},
		{"mark read", func(r *http.Request) error { return h.markRead(httptest.NewRecorder(), r) }, "", params, ""},
		{"search", func(r *http.Request) error { return h.search(httptest.NewRecorder(), r) }, "", nil, "q=x"},
		{"search in a channel", func(r *http.Request) error { return h.search(httptest.NewRecorder(), r) }, "", nil, "q=x&channel_id=" + ch},
		{"delete category", func(r *http.Request) error { return h.deleteCategory(httptest.NewRecorder(), r) }, "", catParams, ""},
		{"rename category", func(r *http.Request) error { return h.renameCategory(httptest.NewRecorder(), r) }, `{"name":"y"}`, catParams, ""},
		{"create in category", func(r *http.Request) error { return h.createChannel(httptest.NewRecorder(), r) }, `{"name":"n","category_id":"` + cat.ID.String() + `"}`, nil, ""},
		{"layout", func(r *http.Request) error { return h.applyLayout(httptest.NewRecorder(), r) }, `{"categories":[{"id":"` + cat.ID.String() + `","sort_order":1}]}`, nil, ""},
		{"duplicate", func(r *http.Request) error { return h.duplicateChannel(httptest.NewRecorder(), r) }, "", params, ""},
		{"update channel", func(r *http.Request) error { return h.updateChannel(httptest.NewRecorder(), r) }, `{"name":"z"}`, params, ""},
		{"delete channel", func(r *http.Request) error { return h.deleteChannel(httptest.NewRecorder(), r) }, "", params, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := c.run(chatRequest(f.u, c.body, c.params, c.query))
			if err == nil {
				t.Fatal("the handler let the request through without a permission state")
			}
			if _, refusal := httpx.AsAPIError(err); refusal {
				t.Fatalf("a load failure must not look like a refusal: %v", err)
			}
		})
	}
}
