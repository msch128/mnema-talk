package slashcmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/slash"
)

func TestDurationsAreReadAsSeconds(t *testing.T) {
	for in, want := range map[string]int{"30s": 30, "10m": 600, "2h": 7200, "7d": 604800, "2w": 1209600, "5 m": 300} {
		got, err := parseDuration(in)
		if err != nil || got != want {
			t.Errorf("%q: %d, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"", "soon", "10", "-5m", "1.5h", "5x", "99999999d"} {
		if _, err := parseDuration(in); err == nil {
			t.Errorf("%q was accepted", in)
		}
	}
}

func TestEveryCommandIsAValidDefinition(t *testing.T) {
	reg := slash.NewRegistry()
	for _, c := range Commands(Services{}) {
		if err := reg.Add(c); err != nil {
			t.Error(err)
		}
	}
}

// The moderation shortcuts are only offered to members who hold the permission.
func TestModerationCommandsAreOfferedByPermission(t *testing.T) {
	f := perm.NewFixture()
	plain, mod, admin := uuid.New(), uuid.New(), uuid.New()
	f.Assign(mod, f.Role(1, perm.TimeoutMembers|perm.BanMembers))
	f.Assign(admin, f.Role(2, perm.Administrator))
	snap := f.Snapshot()
	offered := func(who uuid.UUID) map[string]bool {
		out := map[string]bool{}
		for _, c := range Commands(Services{}) {
			if c.Available(snap, perm.Subject{ID: who}, uuid.Nil) {
				out[c.Definition().Name] = true
			}
		}
		return out
	}
	if p := offered(plain); p["timeout"] || p["ban"] || p["kick"] || !p["me"] || !p["nick"] {
		t.Errorf("plain member: %v", p)
	}
	if m := offered(mod); !m["timeout"] || !m["ban"] || m["kick"] {
		t.Errorf("moderator: %v", m)
	}
	if a := offered(admin); !a["timeout"] || !a["ban"] || !a["kick"] {
		t.Errorf("administrator: %v", a)
	}
}

// Run checks the permission itself, so a command reached without the menu
// (a crafted request, a future caller) is refused before it touches anything.
func TestModerationCommandsRefuseACallerWithoutThePermission(t *testing.T) {
	svc := Services{Auth: &auth.Handler{}}
	user := &auth.User{ID: uuid.New(), Role: auth.RoleUser}
	req := httptest.NewRequest(http.MethodPost, "/", nil).WithContext(auth.WithUser(context.Background(), user))
	call := slash.Call{Request: req, User: user, Options: map[string]any{"member": uuid.New(), "duration": "5m"}}
	for _, c := range Commands(svc) {
		switch c.Definition().Name {
		case "timeout", "ban", "kick":
			_, err := c.Run(context.Background(), call)
			apiErr, ok := httpx.AsAPIError(err)
			if !ok || apiErr.Status != http.StatusForbidden {
				t.Errorf("/%s without the permission: %v", c.Definition().Name, err)
			}
		}
	}
}

func TestTheTextCommandsRenderTheirMessage(t *testing.T) {
	cases := []struct {
		spec textSpec
		text string
		want string
	}{
		{meSpec, "waves", "_waves_"},
		{spoilerSpec, "twist", "||twist||"},
		{shrugSpec, "", shrugFace},
		{shrugSpec, "meh", "meh " + shrugFace},
		{tableflipSpec, "", tableflipFace},
		{unflipSpec, "ok", "ok " + unflipFace},
	}
	for _, tc := range cases {
		if got := tc.spec.render(tc.text); got != tc.want {
			t.Errorf("/%s %q: %q, want %q", tc.spec.name, tc.text, got, tc.want)
		}
	}
}
