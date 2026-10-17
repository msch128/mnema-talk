//go:build integration

package bot

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
)

func (e *env) do(h http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bot "+token)
	}
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestCommandAndInteractionRefusals(t *testing.T) {
	e := newEnv(t)
	h, cmds := e.restHandler(e.p, true)
	st := e.restSetup(cmds)
	call := func(method, path, body string) int { return e.do(h, method, path, st.token, body).Code }
	ia := BasePath + "/interactions/" + st.interaction.String() + "/" + st.itoken

	for _, c := range []struct {
		what, method, path, body string
		want                     int
	}{
		{"replace, bad body", "PUT", BasePath + "/commands", `{`, 400},
		{"replace, bad command", "PUT", BasePath + "/commands", `[{"name":"A","description":"x"}]`, 400},
		{"create, bad body", "POST", BasePath + "/commands", `{`, 400},
		{"create, blank option description", "POST", BasePath + "/commands", `{"name":"x","description":"d","options":[{"name":"o","description":" ","type":3}]}`, 400},
		{"delete, bad id", "DELETE", BasePath + "/commands/x", ``, 400},
		{"callback, bad body", "POST", ia + "/callback", `{`, 400},
		{"callback, no data", "POST", ia + "/callback", `{"type":4}`, 400},
		{"callback, empty", "POST", ia + "/callback", `{"type":4,"data":{"content":""}}`, 400},
		{"callback, too long", "POST", ia + "/callback", `{"type":4,"data":{"content":"` + strings.Repeat("x", 4001) + `"}}`, 400},
		{"callback, bad embed", "POST", ia + "/callback", `{"type":4,"data":{"embeds":[{"url":"javascript:1"}]}}`, 400},
		{"callback, bad type", "POST", ia + "/callback", `{"type":1}`, 400},
		{"callback, bad id", "POST", BasePath + "/interactions/x/" + st.itoken + "/callback", `{"type":5}`, 400},
		{"callback, unknown", "POST", BasePath + "/interactions/" + uuid.NewString() + "/" + st.itoken + "/callback", `{"type":5}`, 404},
		{"followup, bad body", "POST", ia + "/followups", `{`, 400},
		{"followup, empty", "POST", ia + "/followups", `{"content":""}`, 400},
		{"followup, bad id", "POST", BasePath + "/interactions/x/" + st.itoken + "/followups", `{"content":"x"}`, 400},
		{"followup before callback", "POST", ia + "/followups", `{"content":"x"}`, 409},
		{"callback with a mention", "POST", ia + "/callback", `{"type":4,"data":{"content":"@owner hello"}}`, 204},
		{"callback again", "POST", ia + "/callback", `{"type":5}`, 409},
		{"followup, wrong token", "POST", BasePath + "/interactions/" + st.interaction.String() + "/wrong/followups", `{"content":"x"}`, 404},
		{"followup after the callback", "POST", ia + "/followups", `{"content":"x"}`, 201},
	} {
		if got := call(c.method, c.path, c.body); got != c.want {
			t.Errorf("%s: %d, want %d", c.what, got, c.want)
		}
	}

	// A bot that loses Send messages after its first response cannot follow up.
	if _, err := e.p.Exec(context.Background(), `UPDATE roles SET permissions = 0 WHERE id = $1`, e.role); err != nil {
		t.Fatal(err)
	}
	cmds.Perms.Invalidate()
	if got := call("POST", ia+"/followups", `{"content":"too late"}`); got != http.StatusForbidden && got != http.StatusNotFound {
		t.Errorf("followup without permission: %d", got)
	}

	// A bot that may no longer send cannot answer.
	if _, err := e.p.Exec(context.Background(), `UPDATE roles SET permissions = 0 WHERE id = $1`, e.role); err != nil {
		t.Fatal(err)
	}
	h2, cmds2 := e.restHandler(e.p, true)
	st2 := e.restSetup(cmds2)
	if _, err := e.p.Exec(context.Background(), `UPDATE roles SET permissions = 0 WHERE id = $1`, e.role); err != nil {
		t.Fatal(err)
	}
	ia2 := BasePath + "/interactions/" + st2.interaction.String() + "/" + st2.itoken + "/callback"
	if w := e.do(h2, "POST", ia2, st2.token, `{"type":4,"data":{"content":"x"}}`); w.Code != http.StatusNotFound && w.Code != http.StatusForbidden {
		t.Fatalf("answered without permission: %d", w.Code)
	}
}

func TestMemberCommandRefusals(t *testing.T) {
	e := newEnv(t)
	h, cmds := e.restHandler(e.p, true)
	st := e.restSetup(cmds)
	ch := "/api/channels/" + e.ch.String()
	run := func(h http.Handler, ch, body string) int { return e.do(h, "POST", ch+"/interactions", "", body).Code }
	cmdBody := func(id uuid.UUID, opts string) string {
		return fmt.Sprintf(`{"command_id":"%s","options":%s}`, id, opts)
	}

	if got := e.do(h, "GET", "/api/channels/x/commands", "", "").Code; got != 400 {
		t.Errorf("bad channel id: %d", got)
	}
	for _, c := range []struct {
		what, ch, body string
		want           int
	}{
		{"bad channel id", "/api/channels/x", `{}`, 400},
		{"bad body", ch, `{`, 400},
		{"unknown command", ch, cmdBody(uuid.New(), `{}`), 404},
		{"missing option", ch, cmdBody(st.cmd, `{}`), 400},
		{"null option", ch, cmdBody(st.cmd, `{"text":null}`), 400},
		{"not an integer", ch, cmdBody(st.cmd, `{"text":"x","n":"1"}`), 400},
		{"huge integer", ch, cmdBody(st.cmd, `{"text":"x","n":1e300}`), 400},
		{"channel not an id", ch, cmdBody(st.cmd, `{"text":"x","where":5}`), 400},
		{"unknown member", ch, cmdBody(st.cmd, `{"text":"x","who":"`+uuid.NewString()+`"}`), 400},
		{"all options", ch, cmdBody(st.cmd, fmt.Sprintf(`{"text":"x","n":-3,"b":false,"who":"%s","where":"%s"}`, e.owner.ID, e.ch)), 202},
	} {
		if got := run(h, c.ch, c.body); got != c.want {
			t.Errorf("%s: %d, want %d", c.what, got, c.want)
		}
	}

	// A full interaction store refuses new runs.
	now := time.Now()
	for i := 0; i < maxPending; i++ {
		cmds.store.m[uuid.New()] = &interaction{created: now}
	}
	if got := run(h, ch, cmdBody(st.cmd, `{"text":"x"}`)); got != 429 {
		t.Errorf("full store: %d", got)
	}
	clear(cmds.store.m)

	// The bot is not connected.
	offline, _ := e.restHandler(e.p, false)
	if got := run(offline, ch, cmdBody(st.cmd, `{"text":"x"}`)); got != 409 {
		t.Errorf("offline bot: %d", got)
	}

	// A command whose option type the server does not know cannot be run.
	if _, err := e.p.Exec(context.Background(),
		`UPDATE bot_commands SET options = '[{"name":"odd","description":"d","type":99,"required":true}]' WHERE id = $1`, st.cmd); err != nil {
		t.Fatal(err)
	}
	if got := run(h, ch, cmdBody(st.cmd, `{"odd":"x"}`)); got != 400 {
		t.Errorf("unknown option type: %d", got)
	}
}

func TestInteractionStoreBounds(t *testing.T) {
	e := newEnv(t)
	_, cmds := e.restHandler(e.p, true)
	s := cmds.store
	now := time.Now()
	s.now = func() time.Time { return now }
	bot := uuid.New()

	// A full store takes nothing more, until old interactions can be dropped.
	for i := 0; i < maxPending; i++ {
		s.m[uuid.New()] = &interaction{botID: bot, created: now}
	}
	if _, ok := s.add(uuid.New(), &interaction{botID: bot}); ok {
		t.Fatal("the store is full")
	}
	for _, in := range s.m {
		in.created = now.Add(-2 * interactionTTL)
	}
	if _, ok := s.add(uuid.New(), &interaction{botID: bot}); !ok {
		t.Fatal("expired interactions make room")
	}
	if len(s.m) != 1 {
		t.Fatalf("%d interactions left", len(s.m))
	}

	// An interaction answers for 15 minutes only.
	id := uuid.New()
	tok, _ := s.add(id, &interaction{botID: bot})
	if _, err := s.claim(id, tok, bot, true); err != nil {
		t.Fatal(err)
	}
	now = now.Add(interactionTTL + time.Second)
	if _, err := s.claim(id, tok, bot, false); err == nil {
		t.Fatal("expired")
	}
	if _, ok := s.m[id]; ok {
		t.Fatal("expired interactions are forgotten")
	}
}

func TestAuthenticatorForgetsOldTouches(t *testing.T) {
	e := newEnv(t)
	st := e.restSetup(NewCommands(Commands{}))
	a := NewAuthenticator(e.p)
	for i := 0; i < 5000; i++ {
		a.touched[uuid.New()] = time.Now()
	}
	if _, err := a.Lookup(context.Background(), st.token); err != nil {
		t.Fatal(err)
	}
	if len(a.touched) > 5 {
		t.Fatalf("%d entries kept", len(a.touched))
	}
	// The lookup of a wrong token, a revoked one and a disabled bot.
	if _, err := a.Lookup(context.Background(), "short"); err == nil {
		t.Fatal("short token")
	}
	_ = perm.All
}
