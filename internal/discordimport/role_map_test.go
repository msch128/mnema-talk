package discordimport

import (
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/perm/permtest"
)

func TestMappedRoleIsNotPlannedAndOverwritesPointAtIt(t *testing.T) {
	target := RoleChoice{ID: uuid.New(), Name: "Mods"}
	p := samplePlan(t, Inputs{RoleMap: map[string]RoleChoice{"12": target}})

	if p.Counts.Roles != 2 || len(p.Roles) != 2 {
		t.Fatalf("roles = %d, want 2 (Moderator is mapped)", p.Counts.Roles)
	}
	for _, r := range p.Roles {
		if r.SourceID == "12" {
			t.Fatal("a mapped role must not be created")
		}
	}
	if len(p.MappedRoles) != 1 || p.MappedRoles[0].RoleID != target.ID || p.MappedRoles[0].SourceName != "Moderator" {
		t.Fatalf("mapped = %+v", p.MappedRoles)
	}
	var found bool
	for _, ch := range p.Channels {
		for _, o := range ch.Overwrites {
			if o.RoleSourceID != "12" {
				continue
			}
			found = true
			if o.MappedRoleID == nil || *o.MappedRoleID != target.ID || o.RoleName != "Mods" {
				t.Fatalf("overwrite = %+v, want the mapped role", o)
			}
		}
	}
	if !found {
		t.Fatal("the staff channel lost the overwrite of the mapped role")
	}
	if unmapped := samplePlan(t, Inputs{}); unmapped.Hash == p.Hash {
		t.Fatal("the mapping must change the plan hash")
	}
}

func TestSameNameSuggestionIgnoresCase(t *testing.T) {
	admin := RoleChoice{ID: uuid.New(), Name: "ADMIN"}
	p := samplePlan(t, Inputs{Mappable: []RoleChoice{admin, {ID: uuid.New(), Name: "Other"}}})
	for _, r := range p.Roles {
		switch got := r.SuggestedRoleID; {
		case r.SourceName == "Admin" && (got == nil || *got != admin.ID):
			t.Fatalf("Admin: suggestion = %v", got)
		case r.SourceName != "Admin" && got != nil:
			t.Fatalf("%s: unexpected suggestion %v", r.SourceName, got)
		}
	}
}

func TestResolveRoleMapEnforcesTheRoleRules(t *testing.T) {
	tpl := parseSample(t)
	f := permtest.NewFixture()
	low, mid, high := f.Role(1, perm.ViewChannel), f.Role(5, perm.ViewChannel), f.Role(9, perm.ViewChannel)
	managed := f.ManagedRole(2, perm.ViewChannel)
	botHeld := f.Role(3, perm.ViewChannel)
	actor, bot, owner := uuid.New(), uuid.New(), uuid.New()
	f.Bot(bot).Assign(bot, botHeld).Owner(owner).Assign(actor, mid)
	snap := f.Snapshot()
	member, ownerSub := perm.Subject{ID: actor}, perm.Subject{ID: owner, Owner: true}

	cases := []struct {
		name   string
		sub    perm.Subject
		raw    map[string]string
		status int // 0: accepted
	}{
		{"role below the importer", member, map[string]string{"12": low.String()}, 0},
		{"role at the importer's level", member, map[string]string{"12": mid.String()}, 403},
		{"role above the importer", member, map[string]string{"12": high.String()}, 403},
		{"the Owner may map onto the top role", ownerSub, map[string]string{"12": high.String()}, 0},
		{"managed Bot role", member, map[string]string{"12": managed.String()}, 403},
		{"managed Bot role, even for the Owner", ownerSub, map[string]string{"12": managed.String()}, 403},
		{"role a bot holds", member, map[string]string{"12": botHeld.String()}, 403},
		{"role a bot holds, by the Owner", ownerSub, map[string]string{"12": botHeld.String()}, 0},
		{"@everyone as target", member, map[string]string{"12": f.EveryoneID().String()}, 400},
		{"@everyone as source", member, map[string]string{"0": low.String()}, 400},
		{"unknown source role", member, map[string]string{"999": low.String()}, 400},
		{"not a uuid", member, map[string]string{"12": "nope"}, 400},
		{"role that does not exist", member, map[string]string{"12": uuid.NewString()}, 400},
		{"two roles onto one", member, map[string]string{"11": low.String(), "12": low.String()}, 400},
	}
	for _, c := range cases {
		m, err := resolveRoleMap(tpl, snap, c.sub, c.raw)
		if c.status == 0 {
			if err != nil || len(m) != len(c.raw) {
				t.Errorf("%s: %v, %v", c.name, m, err)
			}
			continue
		}
		if api, ok := httpx.AsAPIError(err); !ok || api.Status != c.status {
			t.Errorf("%s: got %v, want status %d", c.name, err, c.status)
		}
	}
	if m, err := resolveRoleMap(tpl, snap, member, nil); err != nil || m != nil {
		t.Fatalf("empty map = %v, %v", m, err)
	}
}

func TestMappableRolesLeaveOutWhatIsRefused(t *testing.T) {
	f := permtest.NewFixture()
	low, high := f.Role(1, 0), f.Role(9, 0)
	f.ManagedRole(2, 0)
	actor := uuid.New()
	f.Assign(actor, f.Role(5, 0))
	got := mappableRoles(f.Snapshot(), perm.Subject{ID: actor})
	if len(got) != 1 || got[0].ID != low {
		t.Fatalf("mappable = %v, want only the role below (not %v)", got, high)
	}
}
