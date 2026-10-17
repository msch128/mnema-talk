//go:build integration

package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/media"
	"github.com/msch128/mnema-talk/internal/perm"
)

// nthFault fails the (skip+1)-th statement of a pool and remembers its text.
type nthFault struct {
	mu   sync.Mutex
	skip int
	hit  bool
	sql  string
}

func (f *nthFault) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.hit {
		return ctx
	}
	if f.skip > 0 {
		f.skip--
		return ctx
	}
	f.hit, f.sql = true, strings.Join(strings.Fields(data.SQL), " ")
	c, cancel := context.WithCancel(ctx)
	cancel()
	return c
}

func (*nthFault) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func (f *nthFault) pool(e *env) *db.Pool {
	cfg := e.p.Pool.Config()
	cfg.MaxConns, cfg.MinConns, cfg.MinIdleConns = 1, 0, 0
	cfg.ConnConfig.Tracer = f
	p, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		e.t.Fatal(err)
	}
	return &db.Pool{Pool: p}
}

type fakeDeliverer struct{ ok bool }

func (d fakeDeliverer) DeliverInteraction(uuid.UUID, json.RawMessage) bool { return d.ok }

// state is what one iteration of a route needs.
type state struct {
	bot         fixtureBot
	token       string
	msg, own    uuid.UUID
	cmd         uuid.UUID
	interaction uuid.UUID
	itoken      string
	owner       uuid.UUID
	channel     uuid.UUID
}

type restRoute struct {
	name   string
	method string
	path   func(state, *env) string
	body   func(state) (io.Reader, map[string]string)
	ok     int
}

// quirks of routes whose failures do not look like the rest: alt is another
// status a failing statement may produce (code outside this package reports a
// failed lookup as a bad request); member routes answer in the members' error
// shape.
var quirks = map[string]struct {
	alt    int
	member bool
}{
	"reply":           {alt: 400},
	"member commands": {member: true},
	"run command":     {member: true},
}

func (e *env) restSetup(h *Commands) state {
	e.t.Helper()
	ctx := context.Background()
	f := e.newBot()
	if _, err := e.p.Exec(ctx, `UPDATE bots SET allowed_intents = $2 WHERE id = $1`, f.bot.ID, int64(IntentGuildMembers)); err != nil {
		e.t.Fatal(err)
	}
	if _, err := e.p.Exec(ctx, `UPDATE roles SET permissions = $1 WHERE id = $2`, int64(perm.SendMessages|perm.AddReactions|perm.AttachFiles), e.role); err != nil {
		e.t.Fatal(err)
	}
	// A token the test knows.
	secret, hash, hint := NewToken()
	if _, err := e.p.Exec(ctx, `INSERT INTO bot_tokens (bot_id, name, token_hash, hint) VALUES ($1, 'rest', $2, $3)`, f.bot.ID, hash, hint); err != nil {
		e.t.Fatal(err)
	}
	st := state{bot: f, token: secret}
	nm := chat.NewMessage{ChannelID: e.ch, UserID: e.owner.ID, Content: "hello"}
	var err error
	if st.msg, err = chat.CreateMessage(ctx, e.p, nm); err != nil {
		e.t.Fatal(err)
	}
	nm.UserID, nm.Content = f.bot.UserID, "mine"
	if st.own, err = chat.CreateMessage(ctx, e.p, nm); err != nil {
		e.t.Fatal(err)
	}
	cmd, err := UpsertCommand(ctx, e.p, f.bot.ID, CommandInput{Name: "ping", Description: "pong", Options: []Option{
		{Name: "text", Description: "t", Type: OptionString, Required: true},
		{Name: "n", Description: "n", Type: OptionInteger},
		{Name: "b", Description: "b", Type: OptionBoolean},
		{Name: "who", Description: "w", Type: OptionUser},
		{Name: "where", Description: "w", Type: OptionChannel},
	}})
	if err != nil {
		e.t.Fatal(err)
	}
	st.cmd = cmd.ID
	st.interaction = uuid.New()
	tok, ok := h.store.add(st.interaction, &interaction{botID: f.bot.ID, botUser: f.bot.UserID, botName: "helper", channel: e.ch, invoker: e.owner.ID})
	if !ok {
		e.t.Fatal("interaction store full")
	}
	st.itoken = tok
	st.owner, st.channel = e.owner.ID, e.ch
	return st
}

// restHandler wires the bot API like the server does, on pool.
func (e *env) restHandler(pool *db.Pool, deliver bool) (http.Handler, *Commands) {
	perms := perm.NewService(pool)
	rec := &events.Recorder{}
	authn := NewAuthenticator(pool)
	authn.SetLimits(1_000_000, 1_000_000)
	chatH := &chat.Handler{DB: pool, Events: rec, Perms: perms}
	mediaH := &media.Handler{DB: pool, Store: e.store, Bucket: "b", MaxUploadBytes: 1 << 20, Events: rec, Perms: perms}
	cmds := NewCommands(Commands{DB: pool, Perms: perms, Events: rec, Deliver: fakeDeliverer{ok: deliver}})
	r := chi.NewRouter()
	r.Route(BasePath, func(b chi.Router) {
		b.NotFound(NotFound)
		b.MethodNotAllowed(MethodNotAllowed)
		b.Group(func(a chi.Router) {
			a.Use(authn.Middleware)
			send := authn.LimitSends(ChannelParam)
			a.Get("/users/@me", Me)
			a.Group(func(j chi.Router) {
				chatH.MountBot(j, send)
				cmds.MountBot(j)
			})
			mediaH.MountBot(a, send)
		})
	})
	r.Route("/api", func(m chi.Router) {
		m.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				next.ServeHTTP(w, r.WithContext(auth.WithUser(r.Context(), e.owner)))
			})
		})
		cmds.MountMember(m)
	})
	return r, cmds
}

func uploadBody(st state) (io.Reader, map[string]string) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("content", "see")
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", `form-data; name="file"; filename="a.png"`)
	h.Set("Content-Type", "image/png")
	part, _ := mw.CreatePart(h)
	_, _ = part.Write(png)
	_ = mw.Close()
	return &buf, map[string]string{"Content-Type": mw.FormDataContentType()}
}

func js(s string) func(state) (io.Reader, map[string]string) {
	return func(state) (io.Reader, map[string]string) { return jsonBody(s) }
}

func restRoutes() []restRoute {
	ch := func(e *env) string { return BasePath + "/channels/" + e.ch.String() }
	none := func(state) (io.Reader, map[string]string) { return nil, nil }
	return []restRoute{
		{"me", "GET", func(_ state, e *env) string { return BasePath + "/users/@me" }, none, 200},
		{"channels", "GET", func(_ state, e *env) string { return BasePath + "/channels" }, none, 200},
		{"history", "GET", func(_ state, e *env) string { return ch(e) + "/messages" }, none, 200},
		{"get message", "GET", func(s state, e *env) string { return ch(e) + "/messages/" + s.msg.String() }, none, 200},
		{"post", "POST", func(_ state, e *env) string { return ch(e) + "/messages" },
			js(`{"content":"hi @owner","embeds":[{"title":"t"}],"allowed_mentions":{"parse":["users","everyone"]}}`), 201},
		{"post narrowed", "POST", func(_ state, e *env) string { return ch(e) + "/messages" },
			js(`{"content":"hi","allowed_mentions":{"parse":[],"users":["` + uuid.NewString() + `","x"]}}`), 201},
		{"reply", "POST", func(s state, e *env) string { return ch(e) + "/messages" }, func(s state) (io.Reader, map[string]string) {
			return jsonBody(fmt.Sprintf(`{"content":"re","message_reference":{"message_id":"%s"},"parent_id":"%s"}`, s.msg, s.msg))
		}, 201},
		{"edit", "PATCH", func(s state, e *env) string { return ch(e) + "/messages/" + s.own.String() }, js(`{"content":"edited"}`), 200},
		{"delete", "DELETE", func(s state, e *env) string { return ch(e) + "/messages/" + s.own.String() }, none, 204},
		{"react", "PUT", func(s state, e *env) string {
			return ch(e) + "/messages/" + s.msg.String() + "/reactions/%F0%9F%91%8D/@me"
		}, none, 204},
		{"unreact", "DELETE", func(s state, e *env) string {
			return ch(e) + "/messages/" + s.msg.String() + "/reactions/%F0%9F%91%8D/@me"
		}, none, 204},
		{"upload", "POST", func(_ state, e *env) string { return ch(e) + "/upload" }, uploadBody, 201},
		{"members", "GET", func(_ state, e *env) string { return BasePath + "/members" }, none, 200},
		{"member", "GET", func(_ state, e *env) string { return BasePath + "/members/" + e.owner.ID.String() }, none, 200},
		{"list commands", "GET", func(_ state, e *env) string { return BasePath + "/commands" }, none, 200},
		{"replace commands", "PUT", func(_ state, e *env) string { return BasePath + "/commands" }, js(`[{"name":"a","description":"x"},{"name":"ping","description":"y"}]`), 200},
		{"create command", "POST", func(_ state, e *env) string { return BasePath + "/commands" }, js(`{"name":"new","description":"x"}`), 200},
		{"delete command", "DELETE", func(s state, e *env) string { return BasePath + "/commands/" + s.cmd.String() }, none, 204},
		{"member commands", "GET", func(_ state, e *env) string { return "/api/channels/" + e.ch.String() + "/commands" }, none, 200},
		{"run command", "POST", func(_ state, e *env) string { return "/api/channels/" + e.ch.String() + "/interactions" }, func(s state) (io.Reader, map[string]string) {
			return jsonBody(fmt.Sprintf(`{"command_id":"%s","options":{"text":"hi","n":3,"b":true,"who":"%s","where":"%s"}}`, s.cmd, s.owner, s.channel))
		}, 202},
		{"callback", "POST", func(s state, e *env) string {
			return BasePath + "/interactions/" + s.interaction.String() + "/" + s.itoken + "/callback"
		}, js(`{"type":4,"data":{"content":"pong"}}`), 204},
		{"callback ephemeral", "POST", func(s state, e *env) string {
			return BasePath + "/interactions/" + s.interaction.String() + "/" + s.itoken + "/callback"
		}, js(`{"type":4,"data":{"content":"psst","flags":64}}`), 204},
		{"callback deferred", "POST", func(s state, e *env) string {
			return BasePath + "/interactions/" + s.interaction.String() + "/" + s.itoken + "/callback"
		}, js(`{"type":5}`), 204},
	}
}

// failures that only lose a timestamp do not fail the request.
func tolerated(sql string) bool {
	return strings.Contains(sql, "last_used_at = NOW()") || strings.Contains(sql, "last_active_at = NOW()")
}

func (e *env) callRest(h http.Handler, rt restRoute, st state) *httptest.ResponseRecorder {
	body, hdr := rt.body(st)
	req := httptest.NewRequest(rt.method, rt.path(st, e), body)
	req.Header.Set("Authorization", "Bot "+st.token)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// Every database statement of the bot API can fail; the answer is a sanitized
// 500 in Discord's shape (a lost last-used timestamp aside).
func TestBotAPIDatabaseFailures(t *testing.T) {
	e := newEnv(t)
	for _, rt := range restRoutes() {
		t.Run(rt.name, func(t *testing.T) {
			{
				h, cmds := e.restHandler(e.p, true)
				w := e.callRest(h, rt, e.restSetup(cmds))
				if w.Code != rt.ok {
					t.Fatalf("undisturbed: %d, want %d: %s", w.Code, rt.ok, w.Body)
				}
			}
			for n := 0; ; n++ {
				if n > 80 {
					t.Fatal("more than 80 statements")
				}
				f := &nthFault{skip: n}
				// The interaction store lives in the handler: set it up on the handler under test.
				pool := f.pool(e)
				h, cmds := e.restHandler(pool, true)
				// Setup uses the healthy pool but registers into this handler's store.
				st := e.restSetup(cmds)
				f.skip = n
				w := e.callRest(h, rt, st)
				pool.Close()
				if !f.hit {
					if w.Code != rt.ok {
						t.Fatalf("no statement failed, yet status %d, want %d: %s", w.Code, rt.ok, w.Body)
					}
					return
				}
				if tolerated(f.sql) {
					if w.Code != rt.ok {
						t.Fatalf("statement %d (%s) is best effort: status %d, want %d", n+1, f.sql, w.Code, rt.ok)
					}
					continue
				}
				if q := quirks[rt.name]; q.alt != 0 && w.Code == q.alt {
					continue
				}
				if w.Code != http.StatusInternalServerError {
					t.Fatalf("statement %d (%s) failed: status %d, want 500: %s", n+1, f.sql, w.Code, w.Body)
				}
				if quirks[rt.name].member {
					continue
				}
				var body struct {
					Message string `json:"message"`
					Code    int    `json:"code"`
				}
				if json.Unmarshal(w.Body.Bytes(), &body) != nil || body.Message == "" {
					t.Fatalf("statement %d: not a Discord-shaped error: %s", n+1, w.Body)
				}
			}
		})
	}
}
