//go:build integration

package bot

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestManagementRefusals(t *testing.T) {
	e := newEnv(t)
	f := e.newBot()
	h := e.handler(e.p)
	path := "/admin/bots/" + f.bot.ID.String()
	call := func(method, p, body string) int {
		r, hdr := jsonBody(body)
		return e.serve(h, method, p, r, hdr).Code
	}
	for what, c := range map[string]struct {
		method, path, body string
		want               int
	}{
		"bad json":           {"POST", path + "/tokens", `{`, 400},
		"name too long":      {"POST", path + "/tokens", `{"name":"` + strings.Repeat("x", 40) + `"}`, 400},
		"expiry in the past": {"POST", path + "/tokens", `{"expires_at":"2001-01-01T00:00:00Z"}`, 400},
		"unknown token":      {"POST", path + "/tokens/" + uuid.NewString() + "/regenerate", ``, 404},
		"bad token id":       {"DELETE", path + "/tokens/x", ``, 400},
		"bad command id":     {"PATCH", path + "/commands/x", `{"disabled":true}`, 400},
		"bad command body":   {"PATCH", path + "/commands/" + f.cmd.ID.String(), `{`, 400},
		"unknown command":    {"DELETE", path + "/commands/" + uuid.NewString(), ``, 404},
		"bad delete command": {"DELETE", path + "/commands/x", ``, 400},
		"bad regenerate id":  {"POST", path + "/tokens/x/regenerate", ``, 400},
		"unknown bot":        {"GET", "/admin/bots/" + uuid.NewString() + "/access", ``, 404},
		"bad roles body":     {"PUT", path + "/roles", `{`, 400},
		"unknown role":       {"PUT", path + "/roles", `{"role_ids":["` + uuid.NewString() + `"]}`, 400},
		"bad update":         {"PATCH", path, `{"name":" "}`, 400},
		"bad intents":        {"PATCH", path, `{"allowed_intents":-1}`, 400},
		"bad create":         {"POST", "/admin/bots", `{"name":""}`, 400},
		"bad create intents": {"POST", "/admin/bots", `{"name":"x","allowed_intents":2048}`, 400},
		"bad create body":    {"POST", "/admin/bots", `{`, 400},
		"bad update body":    {"PATCH", path, `{`, 400},
	} {
		if got := call(c.method, c.path, c.body); got != c.want {
			t.Errorf("%s: %d, want %d", what, got, c.want)
		}
	}
	h.Media = nil
	body, hdr := avatarBody()
	if w := e.serve(h, "POST", path+"/avatar", body, hdr); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("avatar without storage: %d", w.Code)
	}
	// Too many bots.
	for i := 0; i < MaxBots; i++ {
		if _, err := Create(context.Background(), e.p, "b", 0, e.owner.ID); err != nil {
			break
		}
	}
	if w := e.serve(h, "POST", "/admin/bots", strings.NewReader(`{"name":"one more"}`), map[string]string{"Content-Type": "application/json"}); w.Code != http.StatusConflict {
		t.Fatalf("over the bot limit: %d", w.Code)
	}
}
