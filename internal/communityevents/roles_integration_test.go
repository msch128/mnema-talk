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
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/testutil"
)

type rosterFixture struct {
	p       *db.Pool
	s       *Store
	h       *Handler
	rec     *events.Recorder
	owner   *auth.User
	creator *auth.User
	event   *CommunityEvent
	pusher  *fakeSignupPusher
}

type fakeSignupPusher struct {
	mu   sync.Mutex
	sent []string
}

func (f *fakeSignupPusher) SendEventSignupNotice(_ context.Context, userID, _ uuid.UUID, _, kind, role string, _ time.Time, _ *uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, fmt.Sprintf("%s:%s:%s", userID, kind, role))
	return nil
}

func (f *fakeSignupPusher) got() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.sent...)
}

func newRosterFixture(t *testing.T) *rosterFixture {
	t.Helper()
	p := testutil.DB(t)
	testutil.Reset(t, p)
	f := &rosterFixture{p: p, s: NewStore(p), rec: &events.Recorder{}, pusher: &fakeSignupPusher{}}
	f.owner = f.user(t, "owner", "admin")
	f.creator = f.user(t, "creator", "user")
	f.h = &Handler{DB: p, Store: f.s, Events: f.rec, Perms: perm.NewService(p), Audit: &audit.Log{DB: p}, pusher: f.pusher}
	ev, err := f.s.Create(context.Background(), CreateEventRequest{Title: "Raid night", StartTime: time.Now().Add(48 * time.Hour), Location: ptr("Discord")}, f.creator.ID)
	if err != nil {
		t.Fatal(err)
	}
	f.event = ev
	return f
}

func (f *rosterFixture) user(t *testing.T, name, role string) *auth.User {
	t.Helper()
	u := &auth.User{Username: name, DisplayName: name, Role: role}
	err := f.p.QueryRow(context.Background(), `INSERT INTO users (username, display_name, password_hash, role) VALUES ($1, $2, 'x', $3) RETURNING id, created_at`, name, name, role).Scan(&u.ID, &u.CreatedAt)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func (f *rosterFixture) setRoles(t *testing.T, roles ...RoleInput) []EventRole {
	t.Helper()
	if _, err := f.s.SetRoles(context.Background(), f.event.ID, roles); err != nil {
		t.Fatal(err)
	}
	ev, err := f.s.Get(context.Background(), f.event.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	return ev.Roles
}

func role(name string, capacity int) RoleInput {
	return RoleInput{Name: name, Capacity: &capacity}
}

func (f *rosterFixture) signUp(t *testing.T, u *auth.User, roleID uuid.UUID) Signup {
	t.Helper()
	s, promoted, err := f.s.SignUp(context.Background(), f.event.ID, u.ID, roleID)
	if err != nil {
		t.Fatal(err)
	}
	_ = promoted
	return s
}

func (f *rosterFixture) state(t *testing.T, u *auth.User) string {
	t.Helper()
	var state string
	err := f.p.QueryRow(context.Background(), `SELECT state FROM event_signups WHERE event_id = $1 AND user_id = $2`, f.event.ID, u.ID).Scan(&state)
	if err != nil {
		return "none"
	}
	return state
}

func (f *rosterFixture) call(t *testing.T, u *auth.User, handler func(http.ResponseWriter, *http.Request) error, method, body string, params map[string]string) (*httptest.ResponseRecorder, error) {
	t.Helper()
	req := httptest.NewRequest(method, "/", strings.NewReader(body))
	rc := chi.NewRouteContext()
	rc.URLParams.Add("id", f.event.ID.String())
	for k, v := range params {
		rc.URLParams.Add(k, v)
	}
	ctx := auth.WithUser(req.Context(), u)
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rc))
	rec := httptest.NewRecorder()
	return rec, handler(rec, req)
}

func TestTheLastSlotIsGivenOnce(t *testing.T) {
	f := newRosterFixture(t)
	roles := f.setRoles(t, role("Tank", 1))
	users := make([]*auth.User, 12)
	for i := range users {
		users[i] = f.user(t, fmt.Sprintf("m%d", i), "user")
	}
	var wg sync.WaitGroup
	for _, u := range users {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, err := f.s.SignUp(context.Background(), f.event.ID, u.ID, roles[0].ID); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	ev, err := f.s.Get(context.Background(), f.event.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := ev.Roles[0]; got.Confirmed != 1 || got.Waitlist != 11 {
		t.Fatalf("confirmed %d, waitlist %d; want 1 and 11", got.Confirmed, got.Waitlist)
	}
}

func TestTheWaitlistMovesUpFirstInFirstOut(t *testing.T) {
	f := newRosterFixture(t)
	roles := f.setRoles(t, role("Heal", 1))
	a, b, c := f.user(t, "a", "user"), f.user(t, "b", "user"), f.user(t, "c", "user")
	f.signUp(t, a, roles[0].ID)
	if s := f.signUp(t, b, roles[0].ID); s.State != SignupWaitlist || s.WaitlistPosition != 1 {
		t.Fatalf("b: %+v", s)
	}
	if s := f.signUp(t, c, roles[0].ID); s.WaitlistPosition != 2 {
		t.Fatalf("c: %+v", s)
	}

	_, promoted, err := f.s.Leave(context.Background(), f.event.ID, a.ID)
	if err != nil || len(promoted) != 1 || promoted[0].UserID != b.ID || promoted[0].RoleName != "Heal" {
		t.Fatalf("promoted after a left: %+v %v", promoted, err)
	}
	if f.state(t, b) != SignupConfirmed || f.state(t, c) != SignupWaitlist {
		t.Fatal("b should be confirmed and c still waiting")
	}

	// A capacity increase moves the rest up inside the same call.
	moved, err := f.s.SetRoles(context.Background(), f.event.ID, []RoleInput{{ID: &roles[0].ID, Name: "Heal", Capacity: ptr(3)}})
	if err != nil || len(moved) != 1 || moved[0].UserID != c.ID {
		t.Fatalf("moved after growing: %+v %v", moved, err)
	}
}

func TestSwitchingRolesFreesTheOldSlot(t *testing.T) {
	f := newRosterFixture(t)
	roles := f.setRoles(t, role("Tank", 1), role("DPS", 5))
	a, b := f.user(t, "a", "user"), f.user(t, "b", "user")
	f.signUp(t, a, roles[0].ID)
	f.signUp(t, b, roles[0].ID)
	_, promoted, err := f.s.SignUp(context.Background(), f.event.ID, a.ID, roles[1].ID)
	if err != nil || len(promoted) != 1 || promoted[0].UserID != b.ID {
		t.Fatalf("promoted: %+v %v", promoted, err)
	}
	if f.state(t, a) != SignupConfirmed || f.state(t, b) != SignupConfirmed {
		t.Fatal("both should be confirmed")
	}
	// Signing up for the role one holds changes nothing.
	if s := f.signUp(t, a, roles[1].ID); s.State != SignupConfirmed {
		t.Fatalf("repeat sign-up: %+v", s)
	}
	var rsvp string
	if err := f.p.QueryRow(context.Background(), `SELECT response FROM event_rsvps WHERE event_id = $1 AND user_id = $2`, f.event.ID, a.ID).Scan(&rsvp); err != nil || rsvp != RSVPGoing {
		t.Fatalf("a sign-up counts as going: %q %v", rsvp, err)
	}
}

func TestChangingCapacityAndRemovingRoles(t *testing.T) {
	f := newRosterFixture(t)
	roles := f.setRoles(t, role("DPS", 3), role("Flex", 3))
	a, b, c := f.user(t, "a", "user"), f.user(t, "b", "user"), f.user(t, "c", "user")
	for _, u := range []*auth.User{a, b, c} {
		f.signUp(t, u, roles[0].ID)
	}

	_, err := f.s.SetRoles(context.Background(), f.event.ID, []RoleInput{{ID: &roles[0].ID, Name: "DPS", Capacity: ptr(1)}, {ID: &roles[1].ID, Name: "Flex"}})
	if err == nil || !strings.Contains(err.Error(), "remove 2") {
		t.Fatalf("lowering below the confirmed members must name the number to remove, got %v", err)
	}
	_, err = f.s.SetRoles(context.Background(), f.event.ID, []RoleInput{{ID: &roles[1].ID, Name: "Flex"}})
	if apiErr, ok := httpx.AsAPIError(err); !ok || apiErr.Status != http.StatusConflict {
		t.Fatalf("removing a role with sign-ups is a conflict, got %v", err)
	}
	if _, err := f.s.DeleteRole(context.Background(), f.event.ID, roles[0].ID, nil); err == nil {
		t.Fatal("deleting a role with sign-ups needs move_to")
	}
	if _, err := f.s.DeleteRole(context.Background(), f.event.ID, roles[0].ID, &roles[0].ID); err == nil {
		t.Fatal("move_to cannot be the role itself")
	}
	if _, err := f.s.DeleteRole(context.Background(), f.event.ID, roles[0].ID, ptr(uuid.New())); err == nil {
		t.Fatal("move_to must be a role of the event")
	}
	if _, err := f.s.DeleteRole(context.Background(), f.event.ID, roles[0].ID, &roles[1].ID); err != nil {
		t.Fatal(err)
	}
	ev, _ := f.s.Get(context.Background(), f.event.ID, &a.ID)
	if len(ev.Roles) != 1 || ev.Roles[0].Confirmed != 3 || ev.MySignup == nil || ev.MySignup.RoleID != roles[1].ID {
		t.Fatalf("after the move: %+v mine %+v", ev.Roles, ev.MySignup)
	}
	if _, err := f.s.DeleteRole(context.Background(), f.event.ID, uuid.New(), nil); err == nil {
		t.Fatal("an unknown role is not found")
	}
}

func TestRoleListRulesAreChecked(t *testing.T) {
	f := newRosterFixture(t)
	many := make([]RoleInput, MaxEventRoles+1)
	for i := range many {
		many[i] = RoleInput{Name: fmt.Sprintf("r%d", i)}
	}
	for name, roles := range map[string][]RoleInput{
		"too many":        many,
		"duplicate":       {{Name: "Tank"}, {Name: "tank"}},
		"blank":           {{Name: "  "}},
		"zero capacity":   {{Name: "x", Capacity: ptr(0)}},
		"huge capacity":   {{Name: "x", Capacity: ptr(MaxRoleCapacity + 1)}},
		"long name":       {{Name: strings.Repeat("n", MaxRoleNameLen+1)}},
		"unknown role id": {{ID: ptr(uuid.New()), Name: "ghost"}},
	} {
		if _, err := f.s.SetRoles(context.Background(), f.event.ID, roles); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	// Two roles may swap their names.
	roles := f.setRoles(t, role("Tank", 1), role("Heal", 1))
	swapped := f.setRoles(t, RoleInput{ID: &roles[0].ID, Name: "Heal", Capacity: ptr(1)}, RoleInput{ID: &roles[1].ID, Name: "Tank", Capacity: ptr(1)})
	if swapped[0].Name != "Heal" || swapped[1].Name != "Tank" {
		t.Fatalf("swap: %+v", swapped)
	}
	if _, err := f.s.SetRoles(context.Background(), uuid.New(), nil); err == nil {
		t.Fatal("an unknown event is not found")
	}
}

func TestAFinishedEventHasAReadOnlyRoster(t *testing.T) {
	f := newRosterFixture(t)
	roles := f.setRoles(t, role("Tank", 1))
	a, b := f.user(t, "a", "user"), f.user(t, "b", "user")
	f.signUp(t, a, roles[0].ID)
	f.signUp(t, b, roles[0].ID)
	if _, err := f.s.Cancel(context.Background(), f.event.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.s.SignUp(context.Background(), f.event.ID, f.creator.ID, roles[0].ID); err == nil {
		t.Fatal("no sign-ups on a cancelled event")
	}
	if _, err := f.s.SetRoles(context.Background(), f.event.ID, nil); err == nil {
		t.Fatal("roles are frozen")
	}
	if _, err := f.s.DeleteRole(context.Background(), f.event.ID, roles[0].ID, nil); err == nil {
		t.Fatal("roles are frozen")
	}
	if _, _, err := f.s.RemoveSignup(context.Background(), f.event.ID, a.ID); err == nil {
		t.Fatal("a moderator cannot change a cancelled roster")
	}
	// Leaving is always allowed and promotes nobody on a finished event.
	left, promoted, err := f.s.Leave(context.Background(), f.event.ID, a.ID)
	if err != nil || left == nil || len(promoted) != 0 || f.state(t, b) != SignupWaitlist {
		t.Fatalf("leave: %v %v %v", left, promoted, err)
	}
	if left, _, err := f.s.Leave(context.Background(), f.event.ID, a.ID); err != nil || left != nil {
		t.Fatalf("leaving twice: %v %v", left, err)
	}
	if _, _, err := f.s.Leave(context.Background(), uuid.New(), a.ID); err == nil {
		t.Fatal("an unknown event is not found")
	}
}

func TestWhoMayChangeTheRosterAndSignUp(t *testing.T) {
	f := newRosterFixture(t)
	stranger := f.user(t, "stranger", "user")
	body := `{"roles":[{"name":"Tank","capacity":1}]}`

	if _, err := f.call(t, stranger, f.h.setRoles, http.MethodPut, body, nil); err == nil {
		t.Fatal("a member without Manage events who did not create the event must not set roles")
	}
	// The creator manages the roster without Manage events; the Owner may too.
	if _, err := f.call(t, f.creator, f.h.setRoles, http.MethodPut, body, nil); err != nil {
		t.Fatalf("creator: %v", err)
	}
	if _, err := f.call(t, f.owner, f.h.setRoles, http.MethodPut, `{"roles":[{"name":"Tank","capacity":1},{"name":"DPS"}]}`, nil); err != nil {
		t.Fatalf("owner: %v", err)
	}
	ev, _ := f.s.Get(context.Background(), f.event.ID, nil)
	if len(ev.Roles) != 2 {
		t.Fatalf("roles: %+v", ev.Roles)
	}

	// Everyone who can view the event signs up and leaves; the answer carries their own state.
	rec, err := f.call(t, stranger, f.h.signUp, http.MethodPut, fmt.Sprintf(`{"role_id":%q}`, ev.Roles[0].ID), nil)
	if err != nil {
		t.Fatal(err)
	}
	var got CommunityEvent
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.MySignup == nil || got.MySignup.State != SignupConfirmed || got.RSVPCounts.Going != 1 {
		t.Fatalf("answer: %+v %v", got, err)
	}
	if _, err := f.call(t, f.creator, f.h.signUp, http.MethodPut, `{"role_id":"nope"}`, nil); err == nil {
		t.Fatal("a malformed role id is refused")
	}
	if _, err := f.call(t, f.creator, f.h.signUp, http.MethodPut, fmt.Sprintf(`{"role_id":%q}`, uuid.New()), nil); err == nil {
		t.Fatal("an unknown role is not found")
	}

	// Removing someone else is for the roster's managers; the member is told.
	params := map[string]string{"userID": stranger.ID.String()}
	if _, err := f.call(t, f.creator, f.h.removeSignup, http.MethodDelete, "", map[string]string{"userID": uuid.NewString()}); err == nil {
		t.Fatal("removing someone who is not signed up is not found")
	}
	other := f.user(t, "other", "user")
	if _, err := f.call(t, other, f.h.removeSignup, http.MethodDelete, "", params); err == nil {
		t.Fatal("another member must not remove a sign-up")
	}
	if _, err := f.call(t, f.creator, f.h.removeSignup, http.MethodDelete, "", params); err != nil {
		t.Fatal(err)
	}
	f.h.WaitNotices()
	if got := f.pusher.got(); len(got) != 1 || !strings.Contains(got[0], SignupKindRemoved) {
		t.Fatalf("pushes: %v", got)
	}
	if !hasNotice(f.rec, stranger.ID, SignupKindRemoved) {
		t.Fatal("the removed member gets a WebSocket notice")
	}
	if _, err := f.call(t, other, f.h.leaveSignup, http.MethodDelete, "", nil); err != nil {
		t.Fatalf("leaving without a sign-up is fine: %v", err)
	}
}

func hasNotice(rec *events.Recorder, user uuid.UUID, kind string) bool {
	for _, e := range rec.Snapshot() {
		p, ok := e.Payload.(SignupNoticePayload)
		if e.Type == events.EventSignupNotice && ok && p.Kind == kind && len(e.Recipients) == 1 && e.Recipients[0] == user {
			return true
		}
	}
	return false
}

func TestAMemberWhoMovesUpIsToldAndTheBroadcastKeepsTheRosterPrivate(t *testing.T) {
	f := newRosterFixture(t)
	roles := f.setRoles(t, role("Tank", 1))
	a, b := f.user(t, "a", "user"), f.user(t, "b", "user")
	f.signUp(t, a, roles[0].ID)
	f.signUp(t, b, roles[0].ID)

	if _, err := f.call(t, a, f.h.leaveSignup, http.MethodDelete, "", nil); err != nil {
		t.Fatal(err)
	}
	f.h.WaitNotices()
	if !hasNotice(f.rec, b.ID, SignupKindPromoted) {
		t.Fatal("b should be told they moved up")
	}
	if got := f.pusher.got(); len(got) != 1 || !strings.Contains(got[0], SignupKindPromoted+":Tank") {
		t.Fatalf("pushes: %v", got)
	}
	for _, e := range f.rec.Snapshot() {
		if ev, ok := e.Payload.(*CommunityEvent); ok && (ev.MySignup != nil || ev.Signups != nil) {
			t.Fatalf("a broadcast carries personal data: %+v", ev)
		}
	}
}

func TestATalkEventRosterIsHiddenFromMembersWhoCannotViewTheTalk(t *testing.T) {
	f := newRosterFixture(t)
	var talk uuid.UUID
	if err := f.p.QueryRow(context.Background(), `INSERT INTO channels (name, type) VALUES ('private-talk', 'voice') RETURNING id`).Scan(&talk); err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.Exec(context.Background(), `UPDATE events SET channel_id = $2, location = NULL WHERE id = $1`, f.event.ID, talk); err != nil {
		t.Fatal(err)
	}
	var everyone uuid.UUID
	if err := f.p.QueryRow(context.Background(), `SELECT id FROM roles WHERE is_everyone`).Scan(&everyone); err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.Exec(context.Background(), `INSERT INTO permission_overwrites (channel_id, role_id, deny) VALUES ($1, $2, $3)`, talk, everyone, int64(perm.ViewChannel)); err != nil {
		t.Fatal(err)
	}
	f.h.Perms.Invalidate()
	stranger := f.user(t, "stranger", "user")
	roles := f.setRoles(t, role("Tank", 1))
	if _, err := f.call(t, stranger, f.h.signUp, http.MethodPut, fmt.Sprintf(`{"role_id":%q}`, roles[0].ID), nil); err == nil {
		t.Fatal("a member who cannot view the Talk must not sign up")
	}
	if _, err := f.call(t, stranger, f.h.leaveSignup, http.MethodDelete, "", nil); err == nil {
		t.Fatal("nor reach the event through leaving")
	}
	// A promoted member who lost sight of the Talk is told nothing.
	ev, _ := f.s.Get(context.Background(), f.event.ID, nil)
	f.h.notifyMembers(context.Background(), ev, SignupKindPromoted, "", []Promotion{{EventID: ev.ID, UserID: stranger.ID, RoleName: "Tank"}})
	f.h.WaitNotices()
	if len(f.pusher.got()) != 0 || hasNotice(f.rec, stranger.ID, SignupKindPromoted) {
		t.Fatal("a member who cannot view the Talk is not told")
	}
}

func TestAnErasedMemberFreesTheirSlotForTheNextInLine(t *testing.T) {
	f := newRosterFixture(t)
	roles := f.setRoles(t, role("Tank", 1))
	a, b := f.user(t, "a", "user"), f.user(t, "b", "user")
	f.signUp(t, a, roles[0].ID)
	f.signUp(t, b, roles[0].ID)
	if _, err := f.p.Exec(context.Background(), `DELETE FROM event_signups WHERE user_id = $1`, a.ID); err != nil { // what the erasure does
		t.Fatal(err)
	}
	s := &Scheduler{Handler: f.h}
	s.Tick(context.Background(), time.Now())
	s.Wait()
	if f.state(t, b) != SignupConfirmed || !hasNotice(f.rec, b.ID, SignupKindPromoted) {
		t.Fatal("the scheduler should have moved b up and told them")
	}
	if p, err := f.s.PromoteOpenSlots(context.Background()); err != nil || len(p) != 0 {
		t.Fatalf("nothing is left to move: %v %v", p, err)
	}
}

func TestRoleTemplatesStayHiddenUntilAnAdministratorEnablesThem(t *testing.T) {
	f := newRosterFixture(t)
	member := f.creator
	list := func(u *auth.User) []RoleTemplate {
		rec, err := f.call(t, u, f.h.listTemplates, http.MethodGet, "", nil)
		if err != nil {
			t.Fatal(err)
		}
		var out []RoleTemplate
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	if got := list(member); len(got) != 0 {
		t.Fatalf("members see no template by default: %+v", got)
	}
	presets := list(f.owner)
	if len(presets) < 2 || presets[0].Enabled || !presets[0].Preset {
		t.Fatalf("the owner sees the disabled presets: %+v", presets)
	}
	id := map[string]string{"templateID": presets[0].ID.String()}
	if _, err := f.call(t, member, f.h.updateTemplate, http.MethodPatch, `{"enabled":true}`, id); err == nil {
		t.Fatal("members cannot enable templates")
	}
	if _, err := f.call(t, f.owner, f.h.updateTemplate, http.MethodPatch, `{"enabled":true}`, id); err != nil {
		t.Fatal(err)
	}
	if got := list(member); len(got) != 1 || got[0].ID != presets[0].ID || len(got[0].Roles) == 0 {
		t.Fatalf("an enabled template shows: %+v", got)
	}
	if _, err := f.call(t, f.owner, f.h.updateTemplate, http.MethodPatch, `{"name":"Renamed"}`, id); err == nil {
		t.Fatal("a preset's content is fixed")
	}
	if _, err := f.call(t, f.owner, f.h.deleteTemplate, http.MethodDelete, "", id); err == nil {
		t.Fatal("a preset cannot be deleted")
	}

	// A custom template can be created, renamed and deleted.
	rec, err := f.call(t, f.owner, f.h.createTemplate, http.MethodPost, `{"name":"Movie night","roles":[{"name":"Popcorn","capacity":2}]}`, nil)
	if err != nil {
		t.Fatal(err)
	}
	var custom RoleTemplate
	if err := json.Unmarshal(rec.Body.Bytes(), &custom); err != nil || !custom.Enabled {
		t.Fatalf("created: %+v %v", custom, err)
	}
	cid := map[string]string{"templateID": custom.ID.String()}
	if _, err := f.call(t, f.owner, f.h.createTemplate, http.MethodPost, `{"name":"Movie night","roles":[]}`, nil); err == nil {
		t.Fatal("names are unique")
	}
	if _, err := f.call(t, f.owner, f.h.createTemplate, http.MethodPost, `{"name":"x"}`, nil); err == nil {
		t.Fatal("roles are required")
	}
	if _, err := f.call(t, member, f.h.createTemplate, http.MethodPost, `{"name":"y","roles":[]}`, nil); err == nil {
		t.Fatal("members cannot create templates")
	}
	if _, err := f.call(t, f.owner, f.h.updateTemplate, http.MethodPatch, `{"name":"Cinema","roles":[{"name":"Seat"}]}`, cid); err != nil {
		t.Fatal(err)
	}
	if _, err := f.call(t, f.owner, f.h.updateTemplate, http.MethodPatch, `{"enabled":false}`, map[string]string{"templateID": uuid.NewString()}); err == nil {
		t.Fatal("an unknown template is not found")
	}
	if _, err := f.call(t, f.owner, f.h.deleteTemplate, http.MethodDelete, "", cid); err != nil {
		t.Fatal(err)
	}
	if _, err := f.call(t, f.owner, f.h.deleteTemplate, http.MethodDelete, "", cid); err == nil {
		t.Fatal("deleting twice is not found")
	}
}

func TestDeletingARoleHandsItsSignupsOverThroughTheHandler(t *testing.T) {
	f := newRosterFixture(t)
	roles := f.setRoles(t, role("Tank", 1), role("DPS", 1))
	a, b := f.user(t, "a", "user"), f.user(t, "b", "user")
	f.signUp(t, a, roles[0].ID)
	f.signUp(t, b, roles[1].ID)
	params := func(role uuid.UUID) map[string]string { return map[string]string{"roleID": role.String()} }

	req := httptest.NewRequest(http.MethodDelete, "/?move_to=nope", nil)
	rc := chi.NewRouteContext()
	rc.URLParams.Add("id", f.event.ID.String())
	rc.URLParams.Add("roleID", roles[0].ID.String())
	req = req.WithContext(context.WithValue(auth.WithUser(req.Context(), f.creator), chi.RouteCtxKey, rc))
	if err := f.h.deleteRole(httptest.NewRecorder(), req); err == nil {
		t.Fatal("a malformed move_to is refused")
	}
	if _, err := f.call(t, f.creator, f.h.deleteRole, http.MethodDelete, "", params(roles[0].ID)); err == nil {
		t.Fatal("a role with sign-ups needs move_to")
	}
	req = httptest.NewRequest(http.MethodDelete, "/?move_to="+roles[1].ID.String(), nil)
	req = req.WithContext(context.WithValue(auth.WithUser(req.Context(), f.creator), chi.RouteCtxKey, rc))
	if err := f.h.deleteRole(httptest.NewRecorder(), req); err != nil {
		t.Fatal(err)
	}
	f.h.WaitNotices()
	if f.state(t, a) != SignupWaitlist || f.state(t, b) != SignupConfirmed {
		t.Fatalf("a: %s, b: %s; the full role keeps b and puts a in line", f.state(t, a), f.state(t, b))
	}
}

func TestRosterRoutesAreMountedAndAudited(t *testing.T) {
	f := newRosterFixture(t)
	r := chi.NewRouter()
	f.h.Mount(r)
	for _, route := range [][2]string{
		{http.MethodPut, "/events/x/roles"}, {http.MethodDelete, "/events/x/roles/y"}, {http.MethodPut, "/events/x/signup"},
		{http.MethodDelete, "/events/x/signup"}, {http.MethodDelete, "/events/x/signup/y"},
		{http.MethodGet, "/event-role-templates"}, {http.MethodPost, "/event-role-templates"},
		{http.MethodPatch, "/event-role-templates/x"}, {http.MethodDelete, "/event-role-templates/x"},
	} {
		if !r.Match(chi.NewRouteContext(), route[0], route[1]) {
			t.Errorf("%s %s is not mounted", route[0], route[1])
		}
	}
	f.setRoles(t, role("Tank", 1))
	if _, err := f.call(t, f.creator, f.h.setRoles, http.MethodPut, `{"roles":[{"name":"Tank","capacity":2}]}`, nil); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := f.p.QueryRow(context.Background(), `SELECT COUNT(*) FROM audit_log WHERE action = 'event.edit' AND details->>'roster' = 'roles_set'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("roster audit entries: %d %v", n, err)
	}
	if _, err := f.call(t, f.owner, f.h.createTemplate, http.MethodPost, `{"name":"T","roles":[]}`, nil); err != nil {
		t.Fatal(err)
	}
	if err := f.p.QueryRow(context.Background(), `SELECT COUNT(*) FROM audit_log WHERE action = 'event.role_template_create'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("template audit entries: %d %v", n, err)
	}
}

func TestNoticesNeedNeitherPushNorEventsAndNameTheFallbackRole(t *testing.T) {
	f := newRosterFixture(t)
	bare := &Handler{DB: f.p, Store: f.s}
	ev, _ := f.s.Get(context.Background(), f.event.ID, nil)
	bare.notifyMembers(context.Background(), ev, SignupKindPromoted, "Tank", []Promotion{{EventID: ev.ID, UserID: f.creator.ID}})
	bare.notifyMembers(context.Background(), ev, SignupKindPromoted, "Tank", nil)
	bare.WaitNotices()

	withEvents := &Handler{DB: f.p, Store: f.s, Events: f.rec}
	withEvents.notifyMembers(context.Background(), ev, SignupKindRemoved, "Healer", []Promotion{{EventID: ev.ID, UserID: f.creator.ID}})
	for _, e := range f.rec.Snapshot() {
		if p, ok := e.Payload.(SignupNoticePayload); ok && p.RoleName == "Healer" && p.Kind == SignupKindRemoved {
			return
		}
	}
	t.Fatal("the notice should carry the fallback role name")
}
