//go:build integration

package server

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/perm"
)

type viewAsWorld struct {
	admin, roleAdmin *client
	everyone         uuid.UUID
	general, secret  uuid.UUID
	lounge, staff    uuid.UUID
	insider, muted   uuid.UUID
}

// newViewAsWorld builds a server with a public text channel, a hidden one, an
// open Talk and a Talk only the Insider role may see. roleAdmin holds Manage
// roles but cannot see the hidden channel or the staff Talk.
func newViewAsWorld(t *testing.T) viewAsWorld {
	t.Helper()
	a := newApp(t, false)
	w := viewAsWorld{admin: a.seedAdmin()}
	w.everyone = everyoneID(t, w.admin)
	w.general = a.createChannel(w.admin, "general", "text")
	w.secret = a.createChannel(w.admin, "secret", "text")
	w.lounge = a.createChannel(w.admin, "lounge", "voice")
	w.staff = a.createChannel(w.admin, "staff", "voice")
	w.insider = createRole(t, w.admin, "Insider", 0)
	w.muted = createRole(t, w.admin, "Muted", 0)
	for _, ch := range []uuid.UUID{w.secret, w.staff} {
		wantStatus(t, "hide", putOverwrites(w.admin, "channels", ch,
			overwrite("role", w.everyone, 0, perm.ViewChannel),
			overwrite("role", w.insider, perm.ViewChannel, 0)), http.StatusNoContent)
	}
	wantStatus(t, "deny connect", putOverwrites(w.admin, "channels", w.lounge,
		overwrite("role", w.muted, 0, perm.Connect)), http.StatusNoContent)

	w.roleAdmin = a.register(w.admin, "roleadmin")
	assignRoles(t, w.admin, w.roleAdmin, createRole(t, w.admin, "Role admin", perm.ManageRoles))
	return w
}

func (w viewAsWorld) viewAs(t *testing.T, c *client, role uuid.UUID) chat.ViewAsRole {
	t.Helper()
	res := c.get("/api/admin/roles/" + role.String() + "/view-as")
	wantStatus(t, "view as", res, http.StatusOK)
	var out chat.ViewAsRole
	res.decode(t, &out)
	return out
}

func (w viewAsWorld) channels(out chat.ViewAsRole) map[uuid.UUID]chat.ViewAsChannel {
	m := map[uuid.UUID]chat.ViewAsChannel{}
	for _, ch := range out.Uncategorized {
		m[ch.ID] = ch
	}
	for _, cat := range out.Categories {
		for _, ch := range cat.Channels {
			m[ch.ID] = ch
		}
	}
	return m
}

func TestViewAsRoleShowsWhatThatRoleCanSeeAndJoin(t *testing.T) {
	w := newViewAsWorld(t)

	everyone := w.channels(w.viewAs(t, w.admin, w.everyone))
	if _, ok := everyone[w.secret]; ok {
		t.Error("@everyone must not see the hidden channel")
	}
	if _, ok := everyone[w.staff]; ok {
		t.Error("@everyone must not see the staff Talk")
	}
	if g := everyone[w.general]; !slices.Contains(g.Permissions, "send_messages") || g.CanJoin {
		t.Errorf("general for @everyone: %+v", g)
	}
	if l := everyone[w.lounge]; !l.CanJoin || !slices.Contains(l.Permissions, "speak") {
		t.Errorf("lounge for @everyone: %+v", l)
	}

	insider := w.channels(w.viewAs(t, w.admin, w.insider))
	if _, ok := insider[w.secret]; !ok {
		t.Error("the Insider role sees the hidden channel")
	}
	if s := insider[w.staff]; !s.CanJoin {
		t.Errorf("staff Talk for Insider: %+v", s)
	}

	muted := w.channels(w.viewAs(t, w.admin, w.muted))
	if l, ok := muted[w.lounge]; !ok || l.CanJoin || slices.Contains(l.Permissions, "connect") {
		t.Errorf("a Talk the role may see but not join: %+v (visible %v)", l, ok)
	}
}

func TestViewAsRoleNeverShowsMoreThanTheCallerCanSee(t *testing.T) {
	w := newViewAsWorld(t)
	// The owner sees the hidden channel through the Insider role; the role
	// administrator does not hold it and must not learn that it exists.
	if _, ok := w.channels(w.viewAs(t, w.admin, w.insider))[w.secret]; !ok {
		t.Fatal("test set-up: the owner should see the channel as Insider")
	}
	got := w.viewAs(t, w.roleAdmin, w.insider)
	seen := w.channels(got)
	for _, hidden := range []uuid.UUID{w.secret, w.staff} {
		if _, ok := seen[hidden]; ok {
			t.Fatalf("the preview leaked channel %s that the caller cannot see", hidden)
		}
	}
	if _, ok := seen[w.general]; !ok {
		t.Error("channels both can see must stay")
	}
	raw := w.roleAdmin.get("/api/admin/roles/" + w.insider.String() + "/view-as")
	for _, name := range []string{"secret", "staff"} {
		if slices.Contains(chanNames(got), name) || strings.Contains(string(raw.body), name) {
			t.Errorf("the name %q appears in the response", name)
		}
	}
}

func chanNames(out chat.ViewAsRole) []string {
	var names []string
	for _, ch := range out.Uncategorized {
		names = append(names, ch.Name)
	}
	for _, cat := range out.Categories {
		for _, ch := range cat.Channels {
			names = append(names, ch.Name)
		}
	}
	return names
}

func TestViewAsRoleNeedsManageRolesAndAKnownRole(t *testing.T) {
	w := newViewAsWorld(t)
	a := w.admin.a
	plain := a.register(w.admin, "plain")
	wantStatus(t, "plain member", plain.get("/api/admin/roles/"+w.insider.String()+"/view-as"), http.StatusForbidden)
	wantStatus(t, "unknown role", w.admin.get("/api/admin/roles/"+uuid.NewString()+"/view-as"), http.StatusNotFound)
	wantStatus(t, "bad id", w.admin.get("/api/admin/roles/nope/view-as"), http.StatusBadRequest)
	wantStatus(t, "anonymous", a.anon().get("/api/admin/roles/"+w.insider.String()+"/view-as"), http.StatusUnauthorized)
}

func TestViewAsAnAdministratorRoleIgnoresOverwritesButOnlyWithinTheCallersView(t *testing.T) {
	w := newViewAsWorld(t)
	boss := createRole(t, w.admin, "Boss", perm.Administrator)

	out := w.viewAs(t, w.admin, boss)
	if !out.Administrator {
		t.Error("administrator flag missing")
	}
	if _, ok := w.channels(out)[w.secret]; !ok {
		t.Error("an Administrator role sees every channel the caller sees")
	}
	if _, ok := w.channels(w.viewAs(t, w.roleAdmin, boss))[w.secret]; ok {
		t.Error("the role administrator must still not see the hidden channel")
	}
}
