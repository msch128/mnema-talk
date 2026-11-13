package mnema

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type recorded struct {
	method, path, query, ctype string
	header                     http.Header
	body                       []byte
}

// restServer answers every request with handle and records it.
func restServer(t *testing.T, handle func(w http.ResponseWriter, r *http.Request, n int)) (*REST, func() []recorded) {
	t.Helper()
	var mu sync.Mutex
	var reqs []recorded
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(b))
		mu.Lock()
		reqs = append(reqs, recorded{r.Method, r.URL.EscapedPath(), r.URL.RawQuery, r.Header.Get("Content-Type"), r.Header.Clone(), b})
		n := len(reqs)
		mu.Unlock()
		handle(w, r, n)
	}))
	t.Cleanup(srv.Close)
	r, err := NewREST(Options{BaseURL: srv.URL + "/", Token: testToken})
	if err != nil {
		t.Fatal(err)
	}
	return r, func() []recorded {
		mu.Lock()
		defer mu.Unlock()
		return append([]recorded(nil), reqs...)
	}
}

func TestRequestsCarryTheBotTokenAndTheSDKUserAgent(t *testing.T) {
	r, reqs := restServer(t, func(w http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(w, 200, Self{ID: "u", BotID: "b", Username: "bot-x", Bot: true, AllowedIntents: 513})
	})
	me, err := r.Me(context.Background())
	if err != nil || me.BotID != "b" || me.AllowedIntents != 513 {
		t.Fatalf("Me = %+v, %v", me, err)
	}
	got := reqs()[0]
	if got.method != "GET" || got.path != "/api/bot/v1/users/@me" {
		t.Fatalf("request = %s %s", got.method, got.path)
	}
	if got.header.Get("Authorization") != "Bot "+testToken || !strings.HasPrefix(got.header.Get("User-Agent"), "mnema-talk-sdk-go/") {
		t.Fatalf("headers = %v", got.header)
	}
}

func TestCreateMessageSendsTheDocumentedBody(t *testing.T) {
	r, reqs := restServer(t, func(w http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(w, 201, Message{ID: "m", ChannelID: "c"})
	})
	m, err := r.CreateMessage(context.Background(), "c", MessageCreate{
		Content: "hi", Embeds: []Embed{{Title: "Run 42", Color: 65280}},
		Components:       []Component{ActionRow(PrimaryButton("again", "Again"))},
		AllowedMentions:  &AllowedMentions{Parse: []string{}},
		MessageReference: &MessageReference{MessageID: "m0"}, ParentID: "p",
	})
	if err != nil || m.ID != "m" {
		t.Fatalf("CreateMessage = %+v, %v", m, err)
	}
	got := reqs()[0]
	if got.method != "POST" || got.path != "/api/bot/v1/channels/c/messages" || got.ctype != "application/json" {
		t.Fatalf("request = %s %s %s", got.method, got.path, got.ctype)
	}
	want := `{"content":"hi","embeds":[{"title":"Run 42","color":65280}],"components":[{"type":1,"components":[{"type":2,"custom_id":"again","style":1,"label":"Again"}]}],"allowed_mentions":{},"message_reference":{"message_id":"m0"},"parent_id":"p"}`
	if string(got.body) != want {
		t.Fatalf("body = %s\nwant %s", got.body, want)
	}
}

func TestEditMessageSendsOnlyTheChangedFields(t *testing.T) {
	r, reqs := restServer(t, func(w http.ResponseWriter, _ *http.Request, _ int) { writeJSON(w, 200, Message{ID: "m"}) })
	if _, err := r.EditMessage(context.Background(), "c", "m", MessageEdit{Components: &[]Component{}}); err != nil {
		t.Fatal(err)
	}
	if got := reqs()[0]; got.method != "PATCH" || string(got.body) != `{"components":[]}` {
		t.Fatalf("request = %s %s", got.method, got.body)
	}
}

func TestRoutesUseTheirDocumentedPaths(t *testing.T) {
	r, reqs := restServer(t, func(w http.ResponseWriter, r *http.Request, _ int) {
		if r.Method == "DELETE" || r.Method == "PUT" && strings.Contains(r.URL.Path, "reactions") {
			w.WriteHeader(204)
			return
		}
		_, _ = io.WriteString(w, `[]`)
	})
	ctx := context.Background()
	_, _ = r.Messages(ctx, "c", MessageQuery{Limit: 10, Around: "m"})
	_ = r.AddReaction(ctx, "c", "m", "👍")
	_ = r.RemoveReaction(ctx, "c", "m", "👍")
	_ = r.DeleteMessage(ctx, "c", "m")
	_, _ = r.Members(ctx)
	_, _ = r.SetCommands(ctx, []CommandInput{{Name: "ping", Description: "Pong"}})
	_ = r.DeleteCommand(ctx, "cmd")
	want := []string{
		"GET /api/bot/v1/channels/c/messages?around=m&limit=10",
		"PUT /api/bot/v1/channels/c/messages/m/reactions/%F0%9F%91%8D/@me",
		"DELETE /api/bot/v1/channels/c/messages/m/reactions/%F0%9F%91%8D/@me",
		"DELETE /api/bot/v1/channels/c/messages/m",
		"GET /api/bot/v1/members",
		"PUT /api/bot/v1/commands",
		"DELETE /api/bot/v1/commands/cmd",
	}
	got := reqs()
	for i, w := range want {
		line := got[i].method + " " + got[i].path
		if got[i].query != "" {
			line += "?" + got[i].query
		}
		if line != w {
			t.Errorf("request %d = %s, want %s", i, line, w)
		}
	}
	if b := string(got[5].body); b != `[{"name":"ping","description":"Pong","options":[]}]` {
		t.Errorf("SetCommands body = %s", b)
	}
}

func TestErrorsMapToRESTErrorWithDiscordCodes(t *testing.T) {
	r, _ := restServer(t, func(w http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(w, 404, map[string]any{"message": "Unknown Channel", "code": 10003})
	})
	_, err := r.Message(context.Background(), "c", "m")
	var re *RESTError
	if !errors.As(err, &re) || re.Status != 404 || re.Code != CodeUnknownChannel || re.Message != "Unknown Channel" {
		t.Fatalf("err = %v", err)
	}
}

func TestA429IsWaitedOutAndRetried(t *testing.T) {
	start := time.Now()
	r, reqs := restServer(t, func(w http.ResponseWriter, _ *http.Request, n int) {
		if n == 1 {
			w.Header().Set("Retry-After", "1")
			writeJSON(w, 429, map[string]any{"message": "You are being rate limited.", "retry_after": 0.1, "global": false})
			return
		}
		writeJSON(w, 201, Message{ID: "m"})
	})
	m, err := r.CreateMessage(context.Background(), "c", MessageCreate{Content: "x"})
	if err != nil || m.ID != "m" || len(reqs()) != 2 {
		t.Fatalf("CreateMessage = %+v, %v after %d requests", m, err, len(reqs()))
	}
	if waited := time.Since(start); waited < 100*time.Millisecond || waited > time.Second {
		t.Fatalf("waited %v, want retry_after (100ms) rather than Retry-After", waited)
	}
}

func TestRetryAfterHeaderIsUsedWithoutABody(t *testing.T) {
	d := parseRateLimit(http.Header{"Retry-After": {"2"}, "X-Ratelimit-Global": {"true"}}, nil)
	if d.RetryAfter != 2*time.Second || !d.Global {
		t.Fatalf("rate limit = %+v", d)
	}
}

func TestRetriesAreBoundedAndTheLast429IsReturned(t *testing.T) {
	r, reqs := restServer(t, func(w http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(w, 429, map[string]any{"message": "You are being rate limited.", "retry_after": 0.01, "global": true})
	})
	r.maxRetries = 2
	_, err := r.Me(context.Background())
	var re *RESTError
	if !errors.As(err, &re) || re.Status != 429 || !re.Global || len(reqs()) != 3 {
		t.Fatalf("err = %v after %d requests", err, len(reqs()))
	}
}

func TestWaitingForARateLimitStopsWhenTheContextEnds(t *testing.T) {
	r, reqs := restServer(t, func(w http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(w, 429, map[string]any{"message": "You are being rate limited.", "retry_after": 30, "global": false})
	})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := r.Me(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
	if time.Since(start) > 2*time.Second || len(reqs()) != 1 {
		t.Fatal("the client kept waiting after the context ended")
	}
}

func TestPostsToOneChannelKeepWithinFivePerWindow(t *testing.T) {
	var posts atomic.Int32
	r, _ := restServer(t, func(w http.ResponseWriter, _ *http.Request, _ int) {
		posts.Add(1)
		writeJSON(w, 201, Message{ID: "m"})
	})
	r.limits.post = window{limit: postLimit, every: 300 * time.Millisecond}
	start := time.Now()
	for range 6 {
		if _, err := r.CreateMessage(context.Background(), "c", MessageCreate{Content: "x"}); err != nil {
			t.Fatal(err)
		}
	}
	if waited := time.Since(start); waited < 300*time.Millisecond {
		t.Fatalf("six posts took %v, want the sixth to wait for the window", waited)
	}
	start = time.Now()
	if _, err := r.CreateMessage(context.Background(), "other", MessageCreate{Content: "x"}); err != nil || time.Since(start) > 200*time.Millisecond {
		t.Fatalf("another channel waited: %v", err)
	}
}

func TestBucketKeysSeparateChannelsAndShareMessagePosts(t *testing.T) {
	id := "0b5f6c1e-4a1b-4c3d-9e8f-123456789abc"
	cases := map[string]string{
		"POST /channels/c1/messages":                           "post-message:c1",
		"POST /channels/c1/upload":                             "post-message:c1",
		"GET /channels/c1/messages/" + id:                      "GET /channels/c1/messages/:id",
		"PUT /channels/c1/messages/" + id + "/reactions/x/@me": "PUT /channels/c1/messages/:id/reactions",
		"POST /interactions/" + id + "/tok/callback":           "POST /interactions/:id/:token/callback",
		"DELETE /commands/" + id:                               "DELETE /commands/:id",
	}
	for in, want := range cases {
		method, path, _ := strings.Cut(in, " ")
		if got := bucketKey(method, path); got != want {
			t.Errorf("bucketKey(%s) = %s, want %s", in, got, want)
		}
	}
}

func TestUploadSendsAMultipartForm(t *testing.T) {
	r, reqs := restServer(t, func(w http.ResponseWriter, r *http.Request, _ int) {
		if err := r.ParseMultipartForm(1 << 20); err == nil {
			f, h, _ := r.FormFile("file")
			b, _ := io.ReadAll(f)
			writeJSON(w, 201, Message{ID: h.Filename + ":" + string(b) + ":" + r.FormValue("content") + ":" + r.FormValue("allowed_mentions")})
			return
		}
		w.WriteHeader(400)
	})
	m, err := r.Upload(context.Background(), "c", Upload{Name: "a.txt", Reader: strings.NewReader("data"), Content: "cap", AllowedMentions: &AllowedMentions{Parse: []string{"roles"}}})
	if err != nil || m.ID != `a.txt:data:cap:{"parse":["roles"]}` {
		t.Fatalf("Upload = %+v, %v", m, err)
	}
	if got := reqs()[0]; got.path != "/api/bot/v1/channels/c/upload" || !strings.HasPrefix(got.ctype, "multipart/form-data") {
		t.Fatalf("request = %s %s", got.path, got.ctype)
	}
}

func TestInvalidComponentsAreRefusedBeforeTheRequest(t *testing.T) {
	r, reqs := restServer(t, func(w http.ResponseWriter, _ *http.Request, _ int) { w.WriteHeader(500) })
	_, err := r.CreateMessage(context.Background(), "c", MessageCreate{Components: []Component{PrimaryButton("x", "y").Component()}})
	if !errors.Is(err, ErrInvalidComponent) || len(reqs()) != 0 {
		t.Fatalf("err = %v, %d requests", err, len(reqs()))
	}
}

func TestNewRESTRefusesABadBaseURLOrAMissingToken(t *testing.T) {
	if _, err := NewREST(Options{BaseURL: "talk.example.org", Token: "t"}); err == nil {
		t.Error("a base URL without scheme was accepted")
	}
	if _, err := NewREST(Options{BaseURL: "https://talk.example.org"}); err == nil {
		t.Error("a missing token was accepted")
	}
}

func TestMessagesDecodeComponentsAndEmbeds(t *testing.T) {
	raw := `{"id":"m","content":"","components":[{"type":1,"components":[{"type":3,"custom_id":"env","options":[{"label":"A","value":"a","default":true}]}]}],"embeds":[{"title":"t"}]}`
	var m Message
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatal(err)
	}
	if m.Components[0].Components[0].Options[0].Value != "a" || m.Embeds[0].Title != "t" {
		t.Fatalf("message = %+v", m)
	}
}
