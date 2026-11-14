package perm_test

import (
	"fmt"
	"math/rand/v2"
	"os"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	. "github.com/msch128/mnema-talk/internal/perm"
)

// TestSnapshotAgreesWithTheReferenceModel generates communities and asks the
// Snapshot and the reference model (reference_model_test.go) the same
// questions for every member and every channel: Channel, Global, CanStartDM,
// ViewableChannels (including its "nil means all" promise), HiddenChannels,
// ChannelPermissions, CategoryVisible, EveryoneCanView, MayConnectAnywhere,
// and AsRoleOnly for every role. Smoke: 3,000 communities; PERM_SWEEP=1:
// 200,000 (PERM_SEED shifts the seeds).
func TestSnapshotAgreesWithTheReferenceModel(t *testing.T) {
	n, base := 3000, uint64(0)
	if os.Getenv("PERM_SWEEP") == "1" {
		n = 200_000
	}
	if s, err := strconv.ParseUint(os.Getenv("PERM_SEED"), 10, 64); err == nil {
		base = s
	}
	now := time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)
	defer SetClock(func() time.Time { return now })()
	questions, seen := 0, map[string]int{}
	for i := range n {
		seed := base + uint64(i)
		g := genCommunity(seed, now)
		m := refModel{st: g.st, now: now, loaded: g.loaded}
		snap := NewSnapshot(g.st, g.loaded)
		for f, on := range features(m, g) {
			if on {
				seen[f]++
			}
		}
		var diffs []string
		questions += compareMembers(m, snap, g, &diffs)
		questions += compareViews(m, snap, g, &diffs)
		questions += compareCalls(m, snap, g, &diffs)
		if len(diffs) > 0 {
			t.Fatalf("seed %d: %d disagreements, first: %v\n%s", seed, len(diffs), diffs[0], g.describe())
		}
	}
	if questions < n*20 {
		t.Fatalf("asked only %d questions over %d communities", questions, n)
	}
	for _, f := range featureNames {
		if seen[f] < n/50 {
			t.Errorf("only %d of %d communities had %s: the generator does not reach it", seen[f], n, f)
		}
	}
	t.Logf("%d communities, %d questions, no disagreement; communities per feature: %v", n, questions, seen)
}

var featureNames = []string{"administrator", "owner", "bot", "bot with @everyone overwrite", "timeout running", "rules pending",
	"wait running", "wait over for an unseen member", "member overwrite", "conversation call with a member", "conversations off",
	"call member without View channels", "call with an overwrite", "call outsider with Administrator or Owner"}

// features names what a community exercises, so a generator change that
// stops producing a case fails instead of passing quietly.
func features(m refModel, g community) map[string]bool {
	out := map[string]bool{"conversations off": m.st.DMsDisabled}
	for _, u := range g.users {
		out["administrator"] = out["administrator"] || (!m.owner(u) && m.rolePerms(u, !m.bot(u))&Administrator != 0)
		out["owner"] = out["owner"] || m.owner(u)
		out["bot"] = out["bot"] || m.bot(u)
		out["timeout running"] = out["timeout running"] || m.timedOut(u)
		out["rules pending"] = out["rules pending"] || m.rulesPending(u)
		out["wait running"] = out["wait running"] || m.waits(u) != [3]time.Duration{}
		for _, sc := range m.st.ChannelOv {
			_, has := sc.Members[u]
			out["member overwrite"] = out["member overwrite"] || has
			_, ev := sc.Roles[m.st.EveryoneID]
			out["bot with @everyone overwrite"] = out["bot with @everyone overwrite"] || (ev && m.bot(u))
		}
	}
	unseen := m.waits(uuid.New())
	out["wait over for an unseen member"] = m.st.NewMember != (NewMemberRules{}) && unseen != [3]time.Duration{} &&
		slices.Contains(unseen[:], 0) && (m.st.NewMember.Write > 0 || m.st.NewMember.Speak > 0)
	for _, dm := range m.st.DMTalks {
		out["conversation call with a member"] = out["conversation call with a member"] || len(m.st.DMs[dm]) > 0
	}
	callFeatures(m, g, out)
	return out
}

// callFeatures adds the cases of a conversation's call (H-L6-04): a member
// whom the community's rules for that Talk would not let see it, a call
// carrying overwrites, and the Owner or an Administrator outside the
// conversation.
func callFeatures(m refModel, g community, out map[string]bool) {
	for call, dm := range m.st.DMTalks {
		_, ov := m.st.ChannelOv[call]
		out["call with an overwrite"] = out["call with an overwrite"] || ov
		for _, u := range g.users {
			privileged := m.owner(u) || m.unheld(u)&Administrator != 0
			switch {
			case m.st.DMs[dm][u] && !privileged && m.community(u, call)&ViewChannel == 0:
				out["call member without View channels"] = true
			case !m.st.DMs[dm][u] && privileged:
				out["call outsider with Administrator or Owner"] = true
			}
		}
	}
}

// compareCalls checks the rule of doc/talks.md for a conversation's call
// directly: every member of the conversation sees it and holds there what
// they hold server-wide for a Talk (Connect, Speak, Video, Send messages,
// Attach files, Add reactions; the speak wait holds the call as a whole);
// nobody else holds anything, the Owner and Administrators included, whatever
// the overwrites say.
func compareCalls(m refModel, snap *Snapshot, g community, diffs *[]string) int {
	asked := 0
	talk := Connect | Speak | Video | SendMessages | AttachFiles | AddReactions
	for call, dm := range m.st.DMTalks {
		for _, u := range append(slices.Clone(g.users), uuid.New()) {
			got := snap.Channel(Subject{ID: u}, call)
			asked++
			if !m.st.DMs[dm][u] {
				if got != 0 {
					*diffs = append(*diffs, fmt.Sprintf("Channel(%s, call %s) = %b outside the conversation", u, call, got))
				}
				continue
			}
			want := talk & m.unheld(u)
			if m.waits(u)[2] > 0 {
				want &^= refCallWait
			}
			if !got.Has(ViewChannel) || got&talk != want {
				*diffs = append(*diffs, fmt.Sprintf("Channel(%s, call %s) = %b for a member, want View channels and %b", u, call, got, want))
			}
		}
	}
	return asked
}

type community struct {
	st                        State
	loaded                    time.Time
	users, roles, chans, cats []uuid.UUID
	dmIDs                     []uuid.UUID
}

// pick returns a random subset of the bits in from.
func pick(r *rand.Rand, from Set, density float64) Set {
	var s Set
	for b := Set(1); b != 0 && b <= from; b <<= 1 {
		if from&b != 0 && r.Float64() < density {
			s |= b
		}
	}
	return s
}

// genCommunity builds a random community: roles (Administrator rarely),
// categories and channels with overwrites, members with roles, an Owner and a
// bot sometimes, timeouts running or over, rules, new-member waits,
// conversations and their calls.
func genCommunity(seed uint64, now time.Time) community {
	r := rand.New(rand.NewPCG(seed, seed*0x9e3779b97f4a7c15+1))
	everyone := uuid.New()
	g := community{loaded: now.Add(-time.Duration(r.IntN(120)) * time.Second)}
	g.st = State{EveryoneID: everyone, Everyone: pick(r, All&^Administrator, 0.6),
		Roles:     map[uuid.UUID]Role{everyone: {ID: everyone, Name: "@everyone", Everyone: true}},
		UserRoles: map[uuid.UUID][]uuid.UUID{}, Owners: map[uuid.UUID]bool{}, Bots: map[uuid.UUID]bool{}, BotsAll: map[uuid.UUID]bool{},
		ChannelCat: map[uuid.UUID]uuid.UUID{}, Stages: map[uuid.UUID]bool{}, Categories: map[uuid.UUID]bool{},
		DMs: map[uuid.UUID]map[uuid.UUID]bool{}, DMTalks: map[uuid.UUID]uuid.UUID{},
		ChannelOv: map[uuid.UUID]Scope{}, CategoryOv: map[uuid.UUID]Scope{},
		Timeouts: map[uuid.UUID]Timeout{}, RulesAccepted: map[uuid.UUID]int{}, Joined: map[uuid.UUID]time.Time{},
		DMsDisabled: r.IntN(6) == 0}
	r2 := g.st.Roles[everyone]
	r2.Permissions = g.st.Everyone
	g.st.Roles[everyone] = r2
	g.genRoles(r)
	g.genChannels(r)
	g.genMembers(r, now)
	g.genOverwrites(r)
	g.genConversations(r)
	return g
}

func (g *community) genRoles(r *rand.Rand) {
	for i := range 1 + r.IntN(4) {
		id := uuid.New()
		p := pick(r, All&^Administrator, 0.3)
		if r.IntN(8) == 0 {
			p |= Administrator
		}
		g.st.Roles[id] = Role{ID: id, Position: i + 1, Permissions: p, Managed: r.IntN(10) == 0}
		g.roles = append(g.roles, id)
	}
}

func (g *community) genChannels(r *rand.Rand) {
	for range r.IntN(3) {
		id := uuid.New()
		g.st.Categories[id] = true
		g.cats = append(g.cats, id)
	}
	for range 1 + r.IntN(5) {
		id, cat := uuid.New(), uuid.Nil
		if len(g.cats) > 0 && r.IntN(3) > 0 {
			cat = g.cats[r.IntN(len(g.cats))]
		}
		g.st.ChannelCat[id] = cat
		g.chans = append(g.chans, id)
	}
}

func (g *community) genMembers(r *rand.Rand, now time.Time) {
	for i := range 2 + r.IntN(5) {
		u := uuid.New()
		g.users = append(g.users, u)
		for _, role := range g.roles {
			if r.IntN(3) == 0 {
				g.st.UserRoles[u] = append(g.st.UserRoles[u], role)
			}
		}
		switch {
		case i == 0 && r.IntN(3) == 0:
			g.st.Owners[u] = true
		case r.IntN(6) == 0:
			g.st.Bots[u] = true
			g.st.BotsAll[u] = r.IntN(2) == 0
		}
		if r.IntN(5) == 0 {
			g.st.Timeouts[u] = Timeout{Until: now.Add(time.Duration(r.IntN(7200)-3600) * time.Second)}
		}
		g.st.RulesAccepted[u] = r.IntN(3)
		if r.IntN(2) == 0 {
			g.st.Joined[u] = now.Add(-time.Duration(r.IntN(7200)) * time.Second)
		}
	}
	g.st.RulesVersion = []int{0, 0, 1, 2}[r.IntN(4)]
	if r.IntN(3) == 0 {
		// A minute is shorter than the snapshot's age now and then, so a member the
		// snapshot has not seen (counted from the load) has waited it out.
		mins := func() time.Duration { return []time.Duration{0, time.Minute, 30 * time.Minute}[r.IntN(3)] }
		g.st.NewMember = NewMemberRules{Write: mins(), React: mins(), Speak: mins()}
	}
}

func (g *community) genScope(r *rand.Rand) Scope {
	sc := Scope{Roles: map[uuid.UUID]AllowDeny{}, Members: map[uuid.UUID]AllowDeny{}}
	ad := func() AllowDeny {
		return AllowDeny{Allow: pick(r, ChannelScoped, 0.25), Deny: pick(r, ChannelScoped, 0.25)}
	}
	for _, role := range append([]uuid.UUID{g.st.EveryoneID}, g.roles...) {
		if r.IntN(3) == 0 {
			sc.Roles[role] = ad()
		}
	}
	for _, u := range g.users {
		if r.IntN(5) == 0 {
			sc.Members[u] = ad()
		}
	}
	return sc
}

func (g *community) genOverwrites(r *rand.Rand) {
	for _, c := range g.cats {
		if r.IntN(2) == 0 {
			g.st.CategoryOv[c] = g.genScope(r)
		}
	}
	for _, ch := range g.chans {
		if r.IntN(2) == 0 {
			g.st.ChannelOv[ch] = g.genScope(r)
		}
	}
}

// genConversations adds up to two conversations, each with a call sometimes
// (a call is a Talk in ChannelCat, as the loader keeps it).
func (g *community) genConversations(r *rand.Rand) {
	for range r.IntN(3) {
		id := uuid.New()
		members := map[uuid.UUID]bool{}
		for _, u := range g.users {
			if r.IntN(2) == 0 {
				members[u] = true
			}
		}
		g.st.DMs[id] = members
		g.dmIDs = append(g.dmIDs, id)
		if r.IntN(2) == 0 {
			call := uuid.New()
			g.st.ChannelCat[call] = uuid.Nil
			g.st.DMTalks[call] = id
			g.chans = append(g.chans, call)
			if r.IntN(3) == 0 {
				g.st.ChannelOv[call] = g.genScope(r)
			}
		}
	}
}

func (g community) describe() string {
	return fmt.Sprintf("everyone=%b roles=%v userRoles=%v owners=%v bots=%v botsAll=%v channels=%v catOv=%v chanOv=%v dms=%v calls=%v timeouts=%v rules=%d accepted=%v newMember=%+v joined=%v loaded=%v dmsOff=%v",
		g.st.Everyone, g.st.Roles, g.st.UserRoles, g.st.Owners, g.st.Bots, g.st.BotsAll, g.st.ChannelCat, g.st.CategoryOv, g.st.ChannelOv,
		g.st.DMs, g.st.DMTalks, g.st.Timeouts, g.st.RulesVersion, g.st.RulesAccepted, g.st.NewMember, g.st.Joined, g.loaded, g.st.DMsDisabled)
}

// compareMembers asks every per-member question for every member.
func compareMembers(m refModel, snap *Snapshot, g community, diffs *[]string) int {
	asked := 0
	note := func(format string, args ...any) { *diffs = append(*diffs, fmt.Sprintf(format, args...)) }
	for _, u := range append(slices.Clone(g.users), uuid.New()) {
		sub := Subject{ID: u}
		if got, want := snap.Global(sub), m.global(u); got != want {
			note("Global(%s) = %b, reference %b", u, got, want)
		}
		if got, want := snap.CanStartDM(sub), !m.st.DMsDisabled && m.unheld(u)&SendDirectMessages != 0; got != want {
			note("CanStartDM(%s) = %v, reference %v", u, got, want)
		}
		for _, ch := range append(slices.Clone(g.chans), g.dmIDs...) {
			if got, want := snap.Channel(sub, ch), m.channel(u, ch); got != want {
				note("Channel(%s, %s) = %b, reference %b (missing %b, extra %b)", u, ch, got, want, want&^got, got&^want)
			}
			asked++
		}
		for _, cat := range g.cats {
			if got, want := snap.CategoryVisible(sub, cat), m.categoryVisible(u, cat); got != want {
				note("CategoryVisible(%s, %s) = %v, reference %v", u, cat, got, want)
			}
		}
		compareLists(m, snap, g, u, note)
		asked += 3
	}
	return asked
}

// compareLists checks the list answers against the per-channel reference.
func compareLists(m refModel, snap *Snapshot, g community, u uuid.UUID, note func(string, ...any)) {
	sub := Subject{ID: u}
	var want, hidden []uuid.UUID
	connect := m.global(u)&Administrator != 0
	for _, ch := range g.chans {
		p := m.channel(u, ch)
		if p&ViewChannel != 0 {
			want = append(want, ch)
		} else {
			hidden = append(hidden, ch)
		}
		connect = connect || p&(ViewChannel|Connect) == ViewChannel|Connect
	}
	got := snap.ViewableChannels(sub)
	if got == nil && len(hidden) > 0 {
		note("ViewableChannels(%s) = nil (all), but the reference hides %v", u, hidden)
	}
	if got != nil && !sameSet(got, want) {
		note("ViewableChannels(%s) = %v, reference %v", u, got, want)
	}
	if h := snap.HiddenChannels(sub); (h != nil || got != nil) && !sameSet(h, hidden) {
		note("HiddenChannels(%s) = %v, reference %v", u, h, hidden)
	}
	perms := snap.ChannelPermissions(sub)
	if len(perms) != len(want) {
		note("ChannelPermissions(%s) has %d channels, reference %d", u, len(perms), len(want))
	}
	if got := snap.MayConnectAnywhere(sub); got != connect {
		note("MayConnectAnywhere(%s) = %v, reference %v", u, got, connect)
	}
}

func sameSet(a, b []uuid.UUID) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	cmp := func(x, y uuid.UUID) int { return slices.Compare(x[:], y[:]) }
	slices.SortFunc(a, cmp)
	slices.SortFunc(b, cmp)
	return slices.Equal(a, b)
}

// compareViews checks EveryoneCanView and the role preview (AsRoleOnly).
func compareViews(m refModel, snap *Snapshot, g community, diffs *[]string) int {
	asked := 0
	note := func(format string, args ...any) { *diffs = append(*diffs, fmt.Sprintf(format, args...)) }
	for _, ch := range g.chans {
		if got, want := snap.EveryoneCanView(ch), m.everyoneSees(ch); got != want {
			note("EveryoneCanView(%s) = %v, reference %v", ch, got, want)
		}
		asked++
	}
	for _, role := range append([]uuid.UUID{g.st.EveryoneID}, g.roles...) {
		view, sub, ok := snap.AsRoleOnly(role)
		if !ok {
			note("AsRoleOnly(%s) not found", role)
			continue
		}
		pm := previewModel(m, role, sub.ID)
		for _, ch := range g.chans {
			if got, want := view.Channel(sub, ch), pm.channel(sub.ID, ch); got != want {
				note("AsRoleOnly(%s).Channel(%s) = %b, reference %b", role, ch, got, want)
			}
			asked++
		}
	}
	return asked
}

// previewModel is the community with one more member, as AsRoleOnly
// documents it: @everyone and the one role, no overwrite of their own, no
// timeout, rules accepted, not the Owner, a bot only for a managed role, and
// nobody else is the Owner or a bot in the copy.
func previewModel(m refModel, role, member uuid.UUID) refModel {
	st := m.st
	st.Owners, st.Bots, st.Timeouts = map[uuid.UUID]bool{}, map[uuid.UUID]bool{}, map[uuid.UUID]Timeout{}
	st.UserRoles = map[uuid.UUID][]uuid.UUID{}
	if !st.Roles[role].Everyone {
		st.UserRoles[member] = []uuid.UUID{role}
	}
	if st.Roles[role].Managed {
		st.Bots[member] = true
	}
	st.RulesAccepted = map[uuid.UUID]int{member: st.RulesVersion}
	return refModel{st: st, now: m.now, loaded: m.loaded}
}
