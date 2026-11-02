package perm_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	. "github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/perm/permtest"
)

func TestCatalogIsConsistent(t *testing.T) {
	seen := Set(0)
	for _, i := range Catalog {
		if i.Bit == 0 || i.Bit&(i.Bit-1) != 0 {
			t.Fatalf("%s is not a single bit: %b", i.Key, i.Bit)
		}
		if seen&i.Bit != 0 {
			t.Fatalf("%s reuses a bit", i.Key)
		}
		seen |= i.Bit
		if i.Name == "" || i.Key == "" {
			t.Fatalf("unnamed permission %b", i.Bit)
		}
	}
	if seen != All || !All.Valid() || (All << 1).Valid() {
		t.Fatalf("All is %b", All)
	}
	// Frozen: these values are stored in databases and sent to clients.
	want := map[string]Set{
		"administrator": 1 << 0, "view_channel": 1 << 1, "send_messages": 1 << 2, "attach_files": 1 << 3,
		"add_reactions": 1 << 4, "manage_messages": 1 << 5, "mention_everyone": 1 << 6, "connect": 1 << 7,
		"speak": 1 << 8, "video": 1 << 9, "disconnect_members": 1 << 10, "manage_channels": 1 << 11,
		"manage_roles": 1 << 12, "create_invites": 1 << 13, "manage_webhooks": 1 << 14, "manage_members": 1 << 15,
		"manage_bots": 1 << 16, "view_audit_log": 1 << 17, "timeout_members": 1 << 18, "ban_members": 1 << 19, "manage_emoji": 1 << 20,
		"manage_reports": 1 << 21, "send_direct_messages": 1 << 22, "kick_members": 1 << 23,
		"manage_stage":  1 << 24,
		"manage_events": 1 << 25, "bypass_new_member_limits": 1 << 26,
	}
	for _, i := range Catalog {
		if want[i.Key] != i.Bit {
			t.Fatalf("%s changed its bit: %d", i.Key, i.Bit)
		}
		delete(want, i.Key)
	}
	if len(want) != 0 {
		t.Fatalf("permissions removed: %v", want)
	}
	// Migration 0017 seeds @everyone with this number, migration 0050 adds Send direct messages.
	if DefaultEveryone != 990|SendDirectMessages {
		t.Fatalf("DefaultEveryone = %d, migrations 0017 and 0050 seed %d", DefaultEveryone, 990|SendDirectMessages)
	}
	if ChannelScoped.Has(ManageRoles) || !ChannelScoped.Has(ViewChannel|Connect) || ChannelScoped.Has(Administrator) {
		t.Fatalf("ChannelScoped = %b", ChannelScoped)
	}
}

func TestSetHelpers(t *testing.T) {
	s := SendMessages | Speak
	if !s.Has(Speak) || s.Has(Speak|Video) || !s.HasAny(Video|Speak) || s.HasAny(Video) {
		t.Fatal("Has/HasAny")
	}
	if got := s.MissingName(Speak); got != "" {
		t.Fatalf("nothing missing, got %q", got)
	}
	if got := s.MissingName(Speak | Video | ManageRoles); got != "share video and screen" {
		t.Fatalf("first missing = %q", got)
	}
	if got := Set(0).MissingName(1 << 40); got != "permission" {
		t.Fatalf("unknown bit = %q", got)
	}
}

func TestEveryoneDefaultsPreserveTodaysBehavior(t *testing.T) {
	f := permtest.NewFixture()
	ch := f.Channel(uuid.Nil)
	member := Subject{ID: uuid.New()}
	got := f.Snapshot().Channel(member, ch)
	want := ViewChannel | SendMessages | AttachFiles | AddReactions | MentionEveryone | Connect | Speak | Video | SendDirectMessages
	if got != want {
		t.Fatalf("a plain member holds %b, want %b", got, want)
	}
	if g := f.Snapshot().Global(member); g.HasAny(AdminArea | Administrator | ManageMessages) {
		t.Fatalf("a plain member holds admin powers: %b", g)
	}
}

func TestOwnerHasEverythingAndNothingRestrictsThem(t *testing.T) {
	f := permtest.NewFixture()
	cat := f.Category()
	ch := f.Channel(cat)
	owner := Subject{ID: uuid.New(), Owner: true}
	// Deny everything to @everyone everywhere and to the owner by member
	// overwrite: the Owner still has it all.
	f.RoleOverwrite(false, cat, f.EveryoneID(), 0, All)
	f.RoleOverwrite(true, ch, f.EveryoneID(), 0, All)
	f.MemberOverwrite(true, ch, owner.ID, 0, All)
	f.Everyone(0)
	snap := f.Snapshot()
	if snap.Global(owner) != All || snap.Channel(owner, ch) != All || !snap.CategoryVisible(owner, cat) {
		t.Fatal("the Owner lost a permission")
	}
	// Known from the snapshot alone, too.
	f.Owner(owner.ID)
	if !snap.IsOwner(Subject{ID: owner.ID}) {
		t.Fatal("owner not recognized by id")
	}
	if snap.TopPosition(owner) <= 1<<30 {
		t.Fatal("the Owner is above every role")
	}
	if snap.Channel(owner, uuid.New()) != All {
		t.Fatal("the Owner passes unknown channels to the handler's own 404")
	}
}

func TestAdministratorShortCircuitsOverwritesButNotTheOwner(t *testing.T) {
	f := permtest.NewFixture()
	ch := f.Channel(uuid.Nil)
	admin := f.Role(5, Administrator)
	u := uuid.New()
	f.Assign(u, admin)
	f.RoleOverwrite(true, ch, f.EveryoneID(), 0, All)
	f.MemberOverwrite(true, ch, u, 0, All)
	snap := f.Snapshot()
	sub := Subject{ID: u}
	if snap.Channel(sub, ch) != All || snap.Global(sub) != All {
		t.Fatal("Administrator must ignore overwrites")
	}
	if err := snap.CanModerate(sub, Subject{ID: uuid.New(), Owner: true}); err != ErrOwnerProtected {
		t.Fatalf("an Administrator must not touch the Owner: %v", err)
	}
}

func TestResolutionOrder(t *testing.T) {
	f := permtest.NewFixture()
	cat := f.Category()
	ch := f.Channel(cat)
	roleA := f.Role(2, SendMessages)
	roleB := f.Role(1, 0)
	user := uuid.New()
	f.Assign(user, roleA).Assign(user, roleB)
	sub := Subject{ID: user}
	snap := f.Snapshot()

	base := snap.Channel(sub, ch)
	if !base.Has(SendMessages) {
		t.Fatal("base should allow sending")
	}

	// @everyone denied at the category: the member loses it...
	f.RoleOverwrite(false, cat, f.EveryoneID(), 0, SendMessages)
	if snap.Channel(sub, ch).Has(SendMessages) {
		t.Fatal("category @everyone deny should apply")
	}
	// ...a role of theirs allowed there gets it back (roles come after @everyone)...
	f.RoleOverwrite(false, cat, roleA, SendMessages, 0)
	if !snap.Channel(sub, ch).Has(SendMessages) {
		t.Fatal("role allow should beat the @everyone deny")
	}
	// ...the channel's own deny for the role beats the category's allow...
	f.RoleOverwrite(true, ch, roleA, 0, SendMessages)
	if snap.Channel(sub, ch).Has(SendMessages) {
		t.Fatal("channel overwrites win over the category's")
	}
	// ...and a member overwrite beats all roles.
	f.MemberOverwrite(true, ch, user, SendMessages, 0)
	if !snap.Channel(sub, ch).Has(SendMessages) {
		t.Fatal("member allow should beat role deny")
	}
	// Roles combine: a deny on one role and an allow on another allow it.
	f2 := permtest.NewFixture()
	ch2 := f2.Channel(uuid.Nil)
	r1, r2 := f2.Role(2, 0), f2.Role(1, 0)
	u2 := uuid.New()
	f2.Assign(u2, r1).Assign(u2, r2)
	f2.RoleOverwrite(true, ch2, r1, 0, AttachFiles)
	f2.RoleOverwrite(true, ch2, r2, AttachFiles, 0)
	if !f2.Snapshot().Channel(Subject{ID: u2}, ch2).Has(AttachFiles) {
		t.Fatal("allow of any role wins among roles")
	}
}

func TestPrivateChannelAndViewGate(t *testing.T) {
	f := permtest.NewFixture()
	ch := f.Channel(uuid.Nil)
	insider := f.Role(1, 0)
	in, out := uuid.New(), uuid.New()
	f.Assign(in, insider)
	f.RoleOverwrite(true, ch, f.EveryoneID(), 0, ViewChannel)
	f.RoleOverwrite(true, ch, insider, ViewChannel, 0)
	snap := f.Snapshot()
	if p := snap.Channel(Subject{ID: out}, ch); p.HasAny(ChannelScoped) {
		t.Fatalf("an outsider keeps channel permissions without view: %b", p)
	}
	if p := snap.Channel(Subject{ID: in}, ch); !p.Has(ViewChannel | SendMessages | Connect) {
		t.Fatalf("an insider lost permissions: %b", p)
	}
	if got := snap.ViewableChannels(Subject{ID: out}); len(got) != 0 {
		t.Fatalf("outsider sees %v", got)
	}
	if got := snap.ViewableChannels(Subject{ID: in}); len(got) != 1 || got[0] != ch {
		t.Fatalf("insider sees %v", got)
	}
	if got := snap.ViewableChannels(Subject{ID: uuid.New(), Owner: true}); got != nil {
		t.Fatalf("the Owner is not filtered: %v", got)
	}
	if got := snap.HiddenChannels(Subject{ID: out}); len(got) != 1 || got[0] != ch {
		t.Fatalf("outsider hidden %v", got)
	}
	if got := snap.HiddenChannels(Subject{ID: in}); len(got) != 0 {
		t.Fatalf("insider hidden %v", got)
	}
	if got := snap.ChannelPermissions(Subject{ID: out}); len(got) != 0 {
		t.Fatalf("outsider channel permissions: %v", got)
	}
	// A global deny of view hides everything the channel does not re-allow.
	f.RoleOverwrite(true, ch, f.EveryoneID(), 0, 0)
	if !snap.Channel(Subject{ID: out}, ch).Has(ViewChannel) {
		t.Fatal("clearing the overwrite shows the channel again")
	}
}

func TestMayConnectAnywhere(t *testing.T) {
	f := permtest.NewFixture()
	ch := f.Channel(uuid.Nil)
	insider := f.Role(1, 0)
	in, out, timedOut, admin := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	f.Assign(in, insider)
	f.Assign(admin, f.Role(2, Administrator))
	f.RoleOverwrite(true, ch, f.EveryoneID(), 0, ViewChannel|Connect)
	f.RoleOverwrite(true, ch, insider, ViewChannel|Connect, 0)
	snap := f.Snapshot()
	if !snap.MayConnectAnywhere(Subject{ID: in}) {
		t.Fatal("a member with view and connect was refused")
	}
	if snap.MayConnectAnywhere(Subject{ID: out}) {
		t.Fatal("a member who cannot view or connect anywhere was allowed")
	}
	if !snap.MayConnectAnywhere(Subject{ID: admin}) || !snap.MayConnectAnywhere(Subject{ID: uuid.New(), Owner: true}) {
		t.Fatal("an Administrator or the Owner was refused")
	}
	// A time-out removes Connect everywhere.
	f2 := permtest.NewFixture()
	f2.Channel(uuid.Nil)
	f2.Timeout(timedOut, time.Now().Add(time.Hour), "")
	if !f2.Snapshot().MayConnectAnywhere(Subject{ID: uuid.New()}) {
		t.Fatal("a plain member was refused with the defaults")
	}
	if f2.Snapshot().MayConnectAnywhere(Subject{ID: timedOut}) {
		t.Fatal("a member in time-out was allowed")
	}
	// No Talk or channel at all: nothing to join.
	if permtest.NewFixture().Snapshot().MayConnectAnywhere(Subject{ID: uuid.New()}) {
		t.Fatal("allowed with no channels")
	}
}

func TestUnknownChannelGivesNothing(t *testing.T) {
	snap := permtest.NewFixture().Snapshot()
	if p := snap.Channel(Subject{ID: uuid.New()}, uuid.New()); p != 0 {
		t.Fatalf("unknown channel: %b", p)
	}
	if snap.KnowsChannel(uuid.New()) || snap.KnowsCategory(uuid.New()) {
		t.Fatal("knows things it was never told")
	}
	if EmptySnapshot().Channel(Subject{ID: uuid.New()}, uuid.New()) != 0 {
		t.Fatal("the empty snapshot grants something")
	}
}

func TestCategoryVisibility(t *testing.T) {
	f := permtest.NewFixture()
	cat := f.Category()
	f.RoleOverwrite(false, cat, f.EveryoneID(), 0, ViewChannel)
	snap := f.Snapshot()
	if snap.CategoryVisible(Subject{ID: uuid.New()}, cat) {
		t.Fatal("hidden category visible")
	}
	admin := f.Role(1, Administrator)
	a := uuid.New()
	f.Assign(a, admin)
	if !snap.CategoryVisible(Subject{ID: a}, cat) {
		t.Fatal("an Administrator sees every category")
	}
}

func TestHierarchy(t *testing.T) {
	f := permtest.NewFixture()
	mod := f.Role(5, ManageRoles|ManageMembers)
	helper := f.Role(3, 0)
	vip := f.Role(7, 0)
	m, h, v, plain, owner := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	f.Assign(m, mod).Assign(h, helper).Assign(v, vip).Owner(owner)
	snap := f.Snapshot()
	actor := Subject{ID: m}
	if snap.TopPosition(actor) != 5 || snap.TopPosition(Subject{ID: plain}) != 0 {
		t.Fatal("top position")
	}
	for _, c := range []struct {
		name   string
		target uuid.UUID
		ok     bool
	}{{"below", h, true}, {"role-less", plain, true}, {"higher", v, false}, {"equal", m, true /* self */}, {"owner", owner, false}} {
		err := snap.CanModerate(actor, Subject{ID: c.target})
		if (err == nil) != c.ok {
			t.Fatalf("%s: err=%v", c.name, err)
		}
	}
	// Peers of equal rank cannot moderate each other.
	peer := uuid.New()
	f.Assign(peer, mod)
	if err := snap.CanModerate(actor, Subject{ID: peer}); err == nil {
		t.Fatal("equal rank must not moderate")
	}
	if err := snap.CanModerate(Subject{ID: owner}, Subject{ID: v}); err != nil {
		t.Fatalf("the Owner moderates everyone: %v", err)
	}
	roleVip, _ := snap.Role(vip)
	roleHelper, _ := snap.Role(helper)
	if snap.CanManageRole(actor, roleVip) == nil || snap.CanManageRole(actor, roleHelper) != nil {
		t.Fatal("manage only roles below your top")
	}
	if snap.CanManageRole(Subject{ID: owner}, roleVip) != nil {
		t.Fatal("the Owner manages every role")
	}
	if got := snap.RoleIDs(m); len(got) != 1 || got[0] != mod {
		t.Fatalf("role ids %v", got)
	}
	roles := snap.Roles()
	if len(roles) != 4 || !roles[len(roles)-1].Everyone || roles[0].ID != vip {
		t.Fatalf("roles not ordered highest first: %v", roles)
	}
}

func TestServiceChecks(t *testing.T) {
	f := permtest.NewFixture()
	ch := f.Channel(uuid.Nil)
	private := f.Channel(uuid.Nil)
	f.RoleOverwrite(true, private, f.EveryoneID(), 0, ViewChannel)
	f.RoleOverwrite(true, ch, f.EveryoneID(), 0, SendMessages)
	svc := f.Service()
	ctx := context.Background()
	sub := Subject{ID: uuid.New()}

	if err := svc.RequireChannel(ctx, sub, ch, ViewChannel|AddReactions); err != nil {
		t.Fatalf("allowed: %v", err)
	}
	err := svc.RequireChannel(ctx, sub, ch, SendMessages)
	if err == nil || err.Error() != "missing permission: send messages" {
		t.Fatalf("missing permission message: %v", err)
	}
	if err := svc.RequireChannel(ctx, sub, private, ViewChannel); err != ErrChannelHidden {
		t.Fatalf("hidden channel must look nonexistent: %v", err)
	}
	if err := svc.RequireChannel(ctx, sub, uuid.New(), ViewChannel); err != ErrChannelHidden {
		t.Fatalf("unknown channel: %v", err)
	}
	if err := svc.RequireGlobal(ctx, sub, ManageRoles); err == nil {
		t.Fatal("a member must not hold Manage roles")
	}
	if err := svc.RequireGlobal(ctx, Subject{ID: sub.ID, Owner: true}, ManageRoles|CreateInvites); err != nil {
		t.Fatalf("owner: %v", err)
	}
	if err := RequireOwner(sub); err == nil {
		t.Fatal("RequireOwner must refuse members")
	}
	if err := RequireOwner(Subject{Owner: true}); err != nil {
		t.Fatal(err)
	}
	// A nil service imposes no limits.
	var none *Service
	if p, err := none.Channel(ctx, sub, ch); err != nil || p != All {
		t.Fatal("nil service")
	}
	if p, err := none.Global(ctx, sub); err != nil || p != All {
		t.Fatal("nil service global")
	}
	none.Invalidate()
	none.Changed(nil)
	// A fixture service has nothing to reload.
	if _, err := svc.ForChannel(ctx, uuid.New()); err != nil {
		t.Fatalf("a fixture keeps answering: %v", err)
	}
	if _, err := permtest.Unloadable().Snapshot(ctx); err == nil {
		t.Fatalf("no state and no database: %v", err)
	}
}
