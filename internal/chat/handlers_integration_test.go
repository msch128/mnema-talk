//go:build integration

package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat/chattest"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/chat/mentions"
	"github.com/msch128/mnema-talk/internal/chat/messages"
	"github.com/msch128/mnema-talk/internal/chat/readstate"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/testutil"
)

func TestMain(m *testing.M) { os.Exit(testutil.Main(m)) }

type chatFixture struct {
	p *db.Pool
	u *auth.User
	c *Channel
	m uuid.UUID
}

func newChatFixture(t *testing.T) chatFixture {
	t.Helper()
	p, u := chattest.NewMember(t)
	c, err := CreateChannelWithStage(context.Background(), p, nil, "coverage-chat", ChannelTypeText, "", 0, false)
	if err != nil {
		t.Fatal(err)
	}
	m, err := messages.CreateMessage(context.Background(), p, NewMessage{ChannelID: c.ID, UserID: u.ID, Content: "hello world"})
	if err != nil {
		t.Fatal(err)
	}
	return chatFixture{p: p, u: u, c: c, m: m}
}

func TestChatCanceledDatabaseOperations(t *testing.T) {
	f := newChatFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	name, topic, limit := "new-name", "new-topic", 0
	cases := []struct {
		name string
		run  func() error
	}{
		{"load channel", func() error { _, e := core.LoadChannel(ctx, f.p, f.c.ID); return e }},
		{"load message ref", func() error { _, e := core.LoadMessageRef(ctx, f.p, f.m); return e }},
		{"hierarchy", func() error { _, _, e := GetServerHierarchy(ctx, f.p); return e }},
		{"category creation", func() error { _, e := CreateCategory(ctx, f.p, "new", 0); return e }},
		{"channel category check", func() error {
			_, e := CreateChannelWithStage(ctx, f.p, &f.c.ID, "new", ChannelTypeText, "", 0, false)
			return e
		}},
		{"channel creation", func() error {
			_, e := CreateChannelWithStage(ctx, f.p, nil, "new", ChannelTypeText, "", 0, false)
			return e
		}},
		{"category deletion", func() error { return DeleteCategory(ctx, f.p, f.c.ID) }},
		{"channel update", func() error { _, e := EditChannel(ctx, f.p, f.c.ID, &name, &topic, &limit, nil, nil, nil); return e }},
		{"category rename", func() error { _, err := RenameCategory(ctx, f.p, f.c.ID, name); return err }},
		{"members", func() error { _, e := core.GetAllMembers(ctx, f.p); return e }},
		{"resolve mentions", func() error { _, e := mentions.ResolveMentionSet(ctx, f.p, "@all", f.u.ID, nil); return e }},
		{"clear mentions", func() error { return mentions.SaveMentionSet(ctx, f.p, f.m, byName(nil)) }},
		{"record mentions", func() error { return mentions.RecordMentions(ctx, f.p, nil, f.m, f.u.ID, "@all") }},
		{"messages", func() error { _, e := messages.GetChannelMessages(ctx, f.p, f.c.ID, HistoryQuery{Limit: 2}); return e }},
		{"messages around", func() error {
			_, e := messages.GetChannelMessages(ctx, f.p, f.c.ID, HistoryQuery{Limit: 2, Around: &f.m})
			return e
		}},
		{"messages before", func() error {
			_, e := messages.GetChannelMessages(ctx, f.p, f.c.ID, HistoryQuery{Limit: 2, Before: &f.m})
			return e
		}},
		{"thread before", func() error {
			_, e := messages.GetThreadReplies(ctx, f.p, f.m, HistoryQuery{Limit: 2, Before: &f.m})
			return e
		}},
		{"message number", func() error { _, e := messages.GetMessageByNumber(ctx, f.p, 1); return e }},
		{"message", func() error { _, e := messages.GetMessage(ctx, f.p, f.m); return e }},
		{"insert", func() error {
			_, e := messages.CreateMessage(ctx, f.p, NewMessage{ChannelID: f.c.ID, UserID: f.u.ID})
			return e
		}},
		{"collect keys", func() error {
			var keys []string
			return messages.CollectKeys(ctx, f.p, &keys, "SELECT s3_key FROM media")
		}},
		{"reaction summary", func() error { _, e := messages.ReactionSummary(ctx, f.p, f.m); return e }},
		{"read states", func() error { _, e := readstate.GetReadStates(ctx, f.p, f.u); return e }},
		{"search", func() error { _, _, e := Search(ctx, f.p, SearchQuery{Text: "hello", Limit: 2}); return e }},
		{"publish", func() error {
			return messages.PublishMessage(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil).WithContext(ctx), f.p, &events.Recorder{}, f.m)
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := c.run(); !errors.Is(err, context.Canceled) {
				t.Fatalf("error = %v; want cancellation", err)
			}
		})
	}
	// Acquisition failure must prevent partially executed transactions.
	for _, run := range []func() error{
		func() error { _, e := DeleteChannel(ctx, f.p, f.c.ID); return e },
		func() error { _, e := DuplicateChannel(ctx, f.p, f.c.ID); return e },
		func() error { return ApplyLayout(ctx, f.p, nil, nil) },
		func() error {
			_, e := messages.InsertMessage(ctx, f.p, nil, NewMessage{ChannelID: f.c.ID, UserID: f.u.ID}, nil)
			return e
		},
		func() error { return messages.EditMessage(ctx, f.p, nil, f.m, f.u.ID, "changed") },
		func() error { _, e := messages.DeleteMessageAs(ctx, f.p, f.m, f.u, f.u.IsAdmin()); return e },
		func() error { _, e := messages.ToggleReactionAs(ctx, f.p, f.m, f.u.ID, "ok", true); return e },
	} {
		if err := run(); !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v; want cancellation", err)
		}
	}
	msg, err := messages.GetMessage(context.Background(), f.p, f.m)
	if err != nil || msg.Content != "hello world" {
		t.Fatalf("canceled operations changed original message: %v, %v", msg, err)
	}
}

func TestChatHandlerValidationAndErrors(t *testing.T) {
	f := newChatFixture(t)
	h := &Handler{DB: f.p, Events: &events.Recorder{}}
	valid := map[string]string{"channelID": f.c.ID.String(), "messageID": f.m.String(), "id": f.c.ID.String(), "number": "1"}
	type handler func(http.ResponseWriter, *http.Request) error
	pathHandlers := []struct {
		name string
		fn   handler
	}{
		{"list messages", h.messages().ListMessages}, {"create message", h.messages().CreateMessage}, {"edit message", h.messages().EditMessage}, {"delete message", h.messages().DeleteMessage}, {"toggle reaction", h.messages().ToggleReaction}, {"thread", h.messages().GetThread}, {"delete category", h.deleteCategory}, {"delete channel", h.deleteChannel}, {"duplicate channel", h.duplicateChannel}, {"update channel", h.updateChannel}, {"rename category", h.renameCategory},
	}
	for _, c := range pathHandlers {
		t.Run(c.name+" invalid path", func(t *testing.T) {
			chattest.RequireError(t, c.fn(httptest.NewRecorder(), chattest.Request(f.u, "{}", nil, "")), "invalid")
		})
	}
	decodeHandlers := []struct {
		name string
		fn   handler
	}{
		{"create message", h.messages().CreateMessage}, {"edit message", h.messages().EditMessage}, {"reaction", h.messages().ToggleReaction}, {"create category", h.createCategory}, {"create channel", h.createChannel}, {"update channel", h.updateChannel}, {"rename category", h.renameCategory}, {"layout", h.applyLayout},
	}
	for _, c := range decodeHandlers {
		t.Run(c.name+" invalid JSON", func(t *testing.T) {
			if e := c.fn(httptest.NewRecorder(), chattest.Request(f.u, "{", valid, "")); e == nil {
				t.Fatal("malformed JSON accepted")
			}
		})
	}
	cases := []struct {
		name        string
		fn          handler
		body, query string
		params      map[string]string
		want        string
	}{
		{"missing channel", h.messages().ListMessages, "", "", map[string]string{"channelID": uuid.NewString()}, "channel not found"},
		{"bad anchor", h.messages().ListMessages, "", "before=bad", valid, "before"},
		{"multiple anchors", h.messages().ListMessages, "", "before=" + f.m.String() + "&after=" + f.m.String(), valid, "only one"},
		{"message bad content", h.messages().CreateMessage, `{"content":""}`, "", valid, "content"},
		{"message missing parent", h.messages().CreateMessage, `{"content":"hi","parent_id":"` + uuid.NewString() + `"}`, "", valid, "parent"},
		{"invalid message ID", h.messages().EditMessage, `{}`, "", map[string]string{"channelID": f.c.ID.String(), "messageID": "bad"}, "invalid"},
		{"missing message", h.messages().EditMessage, `{}`, "", map[string]string{"channelID": f.c.ID.String(), "messageID": uuid.NewString()}, "message not found"},
		{"edit invalid text", h.messages().EditMessage, `{"content":"` + strings.Repeat("a", 4001) + `"}`, "", valid, "content"},
		{"edit empty", h.messages().EditMessage, `{"content":""}`, "", valid, "content"},
		{"reaction invalid", h.messages().ToggleReaction, `{"emoji":"a b"}`, "", valid, "emoji"},
		{"reaction missing", h.messages().ToggleReaction, `{"emoji":"ok"}`, "", map[string]string{"messageID": uuid.NewString()}, "message not found"},
		{"thread missing", h.messages().GetThread, "", "", map[string]string{"messageID": uuid.NewString()}, "message not found"},
		{"thread bad anchor", h.messages().GetThread, "", "before=bad", valid, "before"},
		{"thread dual anchor", h.messages().GetThread, "", "before=" + f.m.String() + "&after=" + f.m.String(), valid, "only one"},
		{"thread root as cursor", h.messages().GetThread, "", "before=" + f.m.String(), valid, "message not found"},
		{"number invalid", h.messages().GetMessageByNumber, "", "", map[string]string{"number": "bad"}, "invalid number"},
		{"number absent", h.messages().GetMessageByNumber, "", "", map[string]string{"number": "99999"}, "message not found"},
		{"category empty", h.createCategory, `{"name":""}`, "", valid, "name"},
		{"category delete absent", h.deleteCategory, "", "", valid, "category not found"},
		{"channel empty", h.createChannel, `{"name":""}`, "", valid, "name"},
		{"channel delete absent", h.deleteChannel, "", "", map[string]string{"id": uuid.NewString()}, "channel not found"},
		{"channel duplicate absent", h.duplicateChannel, "", "", map[string]string{"id": uuid.NewString()}, "channel not found"},
		{"update bad topic", h.updateChannel, `{"topic":"` + strings.Repeat("a", 256) + `"}`, "", valid, "topic"},
		{"rename bad name", h.renameCategory, `{"name":""}`, "", valid, "name"},
		{"search bad ID", h.search, "", "channel_id=bad", valid, "channel_id"},
		{"search empty", h.search, "", "", valid, "search term"},
		{"layout missing category", h.applyLayout, `{"categories":[{"id":"` + uuid.NewString() + `","sort_order":1}]}`, "", valid, "category not found"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			chattest.RequireError(t, c.fn(httptest.NewRecorder(), chattest.Request(f.u, c.body, c.params, c.query)), c.want)
		})
	}
	body, _ := json.Marshal(LayoutRequest{Categories: make([]CategoryOrder, 501)})
	chattest.RequireError(t, h.applyLayout(httptest.NewRecorder(), chattest.Request(f.u, string(body), nil, "")), "too many entries")
	// Database cancellation must also propagate through HTTP handlers.
	for _, c := range []struct {
		name string
		fn   handler
	}{{"members", h.listMembers}, {"channels", h.listChannels}} {
		t.Run(c.name+" database error", func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			r := chattest.Request(f.u, "", valid, "")
			r = r.WithContext(auth.WithUser(ctx, f.u))
			if err := c.fn(httptest.NewRecorder(), r); !errors.Is(err, context.Canceled) {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestChatDeleteCategoryHandlerAndVoicePreference(t *testing.T) {
	f := newChatFixture(t)
	cat, err := CreateCategory(context.Background(), f.p, "category", 0)
	if err != nil {
		t.Fatal(err)
	}
	recorder := &events.Recorder{}
	h := &Handler{DB: f.p, Events: recorder}
	w := httptest.NewRecorder()
	if err := h.deleteCategory(w, chattest.Request(f.u, "", map[string]string{"id": cat.ID.String()}, "")); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusNoContent || len(recorder.Snapshot()) != 1 || recorder.Snapshot()[0].Type != "channels_changed" {
		t.Fatalf("response/event: %d, %v", w.Code, recorder.Snapshot())
	}
	voice := &chatVoiceRecorder{}
	h.Voice = voice
	if h.voiceRooms() != voice {
		t.Fatal("explicit voice rooms were not selected")
	}
	if err := h.deleteChannel(httptest.NewRecorder(), chattest.Request(f.u, "", map[string]string{"id": f.c.ID.String()}, "")); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(voice.closed, []uuid.UUID{f.c.ID}) {
		t.Fatalf("closed rooms = %v", voice.closed)
	}
	objects := &chatObjectFailure{}
	h.Objects = objects
	h.deleteObjects(context.Background(), []string{"test/key"})
	if objects.calls != 1 {
		t.Fatalf("object deletion calls=%d", objects.calls)
	}
}

type chatVoiceRecorder struct{ closed []uuid.UUID }

func (v *chatVoiceRecorder) CloseVoiceChannel(id uuid.UUID) { v.closed = append(v.closed, id) }

type chatObjectFailure struct{ calls int }

func (o *chatObjectFailure) DeleteBatch(context.Context, []string) error {
	o.calls++
	return fmt.Errorf("test object storage unavailable")
}

func TestChatValidationAndReadMarkerBoundaries(t *testing.T) {
	f := newChatFixture(t)
	ctx := context.Background()
	if _, err := CreateChannelWithStage(ctx, f.p, nil, "new", "unsupported", "", 0, false); err == nil {
		t.Fatal("unsupported channel type accepted")
	}
	if _, err := CreateChannelWithStage(ctx, f.p, chattest.Ptr(uuid.New()), "new", ChannelTypeText, "", 0, false); err == nil {
		t.Fatal("missing category accepted")
	}
	if err := messages.EditMessage(ctx, f.p, nil, uuid.New(), f.u.ID, "new"); err == nil {
		t.Fatal("absent message edited")
	}
	if _, err := messages.DeleteMessageAs(ctx, f.p, uuid.New(), f.u, f.u.IsAdmin()); err == nil {
		t.Fatal("absent message deleted")
	}
	if _, err := messages.GetThreadReplies(ctx, f.p, f.m, HistoryQuery{}); err != nil {
		t.Fatal(err)
	}
	if _, err := messages.GetThreadReplies(ctx, f.p, f.m, HistoryQuery{Limit: 10, Before: &f.m}); err != nil {
		t.Fatal(err)
	}
	h := &Handler{DB: f.p, Events: &events.Recorder{}}
	if err := h.messages().CreateMessage(httptest.NewRecorder(), chattest.Request(f.u, `{"content":"hi"}`, map[string]string{"channelID": uuid.NewString()}, "")); err == nil {
		t.Fatal("message posted in absent channel")
	}
	p := chattest.DriftPool(t, f.p, map[string]string{"messages": `SELECT id,number,channel_id,user_id,parent_id,NULL::text content,is_pinned,is_edited,created_at,updated_at,reply_to_id FROM public.messages`})
	if err := (&Handler{DB: p}).messages().ListMessages(httptest.NewRecorder(), chattest.Request(f.u, "", map[string]string{"channelID": f.c.ID.String()}, "")); err == nil {
		t.Fatal("undecodable history accepted")
	}
	if _, _, err := Search(ctx, f.p, SearchQuery{Text: strings.Repeat("a", 201)}); err == nil {
		t.Fatal("too-long query accepted")
	}
	if _, _, err := Search(ctx, f.p, SearchQuery{Text: "one", Has: "unknown"}); err == nil {
		t.Fatal("unknown attachment filter accepted")
	}
	if _, _, err := Search(ctx, f.p, SearchQuery{Text: "a b c d e f g h discarded", Limit: 10}); err != nil {
		t.Fatal(err)
	}
	for _, has := range []string{"file", "image", "link"} {
		if _, _, err := Search(ctx, f.p, SearchQuery{Has: has, Limit: 10}); err != nil {
			t.Fatal(err)
		}
	}
	for _, run := range []func() error{
		func() error {
			_, e := CreateChannelWithStage(ctx, f.p, nil, "name", ChannelTypeText, strings.Repeat("a", 256), 0, false)
			return e
		},
		func() error {
			topic := strings.Repeat("a", 256)
			_, e := EditChannel(ctx, f.p, f.c.ID, nil, &topic, nil, nil, nil, nil)
			return e
		},
		func() error { _, err := RenameCategory(ctx, f.p, uuid.New(), "new"); return err },
		func() error { return ApplyLayout(ctx, f.p, nil, []ChannelPlacement{{ID: uuid.New()}}) },
		func() error { _, e := EditChannel(ctx, f.p, uuid.New(), nil, nil, nil, nil, nil, nil); return e },
	} {
		if err := run(); err == nil {
			t.Fatal("invalid resource accepted")
		}
	}
	if _, err := readstate.MarkRead(ctx, f.p, f.u.ID, f.c.ID, &f.m); err != nil {
		t.Fatal(err)
	}
	if _, err := readstate.MarkRead(ctx, f.p, f.u.ID, f.c.ID, chattest.Ptr(uuid.New())); err == nil {
		t.Fatal("missing read anchor accepted")
	}
	if _, err := readstate.MarkUnread(ctx, f.p, f.u.ID, f.c.ID, uuid.New()); err == nil {
		t.Fatal("missing unread anchor accepted")
	}
	if _, err := messages.NewerThan(ctx, f.p, f.c.ID, f.m, 0); err != nil {
		t.Fatal(err)
	}
	if messages.Deref(nil) != "" {
		t.Fatal("nil text not empty")
	}
}
