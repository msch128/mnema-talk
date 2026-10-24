package discordimport

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

func TestParseCodeAcceptsLinksAndBareCodes(t *testing.T) {
	good := map[string]string{
		"AbCdEf123456":                                  "AbCdEf123456",
		"https://discord.new/AbCdEf123456":              "AbCdEf123456",
		"discord.new/AbCdEf123456/":                     "AbCdEf123456",
		"https://discord.com/template/AbCdEf123456":     "AbCdEf123456",
		"  http://www.discord.com/template/AbCd-Ef_12 ": "AbCd-Ef_12",
	}
	for in, want := range good {
		if got, err := ParseCode(in); err != nil || got != want {
			t.Errorf("ParseCode(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{
		"", "a", "https://evil.example/template/AbCdEf123456", "https://discord.com.evil.example/template/AbCdEf",
		"https://discord.com/other/AbCdEf123456", "https://user:pw@discord.com/template/AbCdEf123456",
		"https://discord.new/AbC/../../x", "AbCdEf 123456", "ftp://discord.new/AbCdEf123456", "../../etc/passwd",
	} {
		if got, err := ParseCode(in); err == nil {
			t.Errorf("ParseCode(%q) = %q, want an error", in, got)
		}
	}
}

func TestPermissionMappingDropsAdministratorAndUnknownBits(t *testing.T) {
	m := mapBits(1<<3|1<<10|1<<11|1<<1|1<<24|1<<60, perm.All)
	want := perm.ViewChannel | perm.SendMessages | perm.KickMembers
	if m.Set != want {
		t.Fatalf("mapped %v, want %v", keys(m.Set), keys(want))
	}
	if !m.AdministratorDropped {
		t.Fatal("the administrator bit must be reported as dropped")
	}
	for _, name := range []string{"ADMINISTRATOR", "MOVE_MEMBERS", "UNKNOWN_BIT_60"} {
		found := false
		for _, d := range m.Dropped {
			found = found || d == name
		}
		if !found {
			t.Errorf("%s missing from dropped %v", name, m.Dropped)
		}
	}
	// What the importing administrator does not hold is dropped, not granted.
	narrow := mapBits(1<<1|1<<10, perm.ViewChannel)
	if narrow.Set != perm.ViewChannel || len(narrow.Dropped) != 1 || narrow.Dropped[0] != "KICK_MEMBERS" {
		t.Fatalf("narrow = %+v", narrow)
	}
	if got := grantable(perm.All); got.Has(perm.Administrator) || got.Has(perm.ManageBots) {
		t.Fatalf("grantable keeps administrator or manage bots: %v", keys(got))
	}
}

func parseSample(t *testing.T) *Template {
	t.Helper()
	var tpl Template
	if err := json.Unmarshal([]byte(sampleTemplate), &tpl); err != nil {
		t.Fatal(err)
	}
	tpl.Code = "AbCdEf123456"
	return &tpl
}

func samplePlan(t *testing.T, in Inputs) *Plan {
	t.Helper()
	if in.Grantable == 0 {
		in.Grantable = grantable(perm.All)
	}
	return BuildPlan(parseSample(t), in)
}

func TestPlanMapsStructureAndRoles(t *testing.T) {
	p := samplePlan(t, Inputs{ExistingRoles: []string{"member"}, ServerName: "Home"})
	if p.Counts != (Counts{Categories: 2, Channels: 7, Roles: 3}) {
		t.Fatalf("counts = %+v", p.Counts)
	}
	if len(p.Skipped) != 1 || !strings.Contains(p.Skipped[0], "a thread") {
		t.Fatalf("skipped = %v", p.Skipped)
	}
	roles := map[string]PlanRole{}
	for _, r := range p.Roles {
		roles[r.SourceName] = r
	}
	if p.Roles[0].SourceName != "Admin" || p.Roles[2].SourceName != "Member" {
		t.Fatalf("roles must be listed highest first: %+v", p.Roles)
	}
	if got := roles["Member"].Name; got != "Member (2)" {
		t.Fatalf("a name that exists already gets a suffix, got %q", got)
	}
	admin := roles["Admin"]
	if admin.Permissions.Has(perm.Administrator) || !admin.Permissions.Has(perm.ManageChannels) || !admin.AdministratorDropped {
		t.Fatalf("admin role = %+v", admin)
	}
	if !p.AdministratorDropped {
		t.Fatal("the plan must say the administrator bit was dropped")
	}
	if mod := roles["Moderator"]; mod.Permissions != perm.KickMembers|perm.BanMembers|perm.ManageMessages || mod.Dropped[0] != "MOVE_MEMBERS" {
		t.Fatalf("moderator = %+v", mod)
	}
	if p.Everyone.Applied || len(p.Everyone.PermissionKeys) == 0 {
		t.Fatalf("@everyone is information only: %+v", p.Everyone)
	}
}

func TestPlanMapsChannelKinds(t *testing.T) {
	p := samplePlan(t, Inputs{})
	by := map[string]PlanChannel{}
	for _, c := range p.Channels {
		by[c.Name] = c
	}
	checks := map[string]string{"rules": "text", "news": "text", "ideas": "text", "general": "text", "Lobby": "voice", "Stage": "voice", "staff": "text"}
	for name, typ := range checks {
		if by[name].Type != typ {
			t.Errorf("%s: type %q, want %q", name, by[name].Type, typ)
		}
	}
	if by["Lobby"].UserLimit != 5 || by["rules"].Topic != "Read first" || by["rules"].SlowModeSeconds != 5 {
		t.Fatalf("limits not kept: %+v %+v", by["Lobby"], by["rules"])
	}
	if got := by["general"].SlowModeSeconds; got <= 0 || got >= 43200 {
		t.Fatalf("slow mode must be capped, got %d", got)
	}
	if !by["staff"].Private || by["rules"].Private {
		t.Fatal("only the staff channel hides from @everyone")
	}
	staff := by["staff"].Overwrites
	if len(staff) != 2 || !staff[0].Everyone || !staff[0].Deny.Has(perm.ViewChannel) || staff[1].Allow != perm.ViewChannel {
		t.Fatalf("staff overwrites (member overwrites are ignored) = %+v", staff)
	}
	// Uncategorized channels come last.
	if last := p.Channels[len(p.Channels)-1]; last.Name != "general" || last.Category != "" {
		t.Fatalf("uncategorized channels go last, got %+v", last)
	}
	// With the newer kinds present they are kept.
	q := samplePlan(t, Inputs{Caps: Capabilities{Forum: true, Stage: true}})
	for _, c := range q.Channels {
		if (c.Name == "ideas" && c.Type != "forum") || (c.Name == "Stage" && c.Type != "stage") {
			t.Errorf("%s: type %q with the capability present", c.Name, c.Type)
		}
	}
}

func TestPlanHashIsStableAndSensitive(t *testing.T) {
	a, b := samplePlan(t, Inputs{}), samplePlan(t, Inputs{})
	if a.Hash == "" || a.Hash != b.Hash {
		t.Fatal("the same input must give the same hash")
	}
	if c := samplePlan(t, Inputs{ExistingRoles: []string{"Member"}}); c.Hash == a.Hash {
		t.Fatal("a changed server state must change the hash")
	}
}

func TestFetchRefusesWhatItShould(t *testing.T) {
	ctx := context.Background()
	ok, hits := serveTemplate(t, sampleTemplate)
	tpl, err := loopbackFetcher(ok.URL).Fetch(ctx, "AbCdEf123456")
	if err != nil || tpl.Guild.Name == "" || *hits != 1 {
		t.Fatalf("fetch = %v, %v, hits %d", tpl, err, *hits)
	}

	// The production client refuses a loopback address: the guard is on.
	guarded := NewFetcher()
	guarded.base = ok.URL + "/"
	if _, err := guarded.Fetch(ctx, "AbCdEf123456"); err == nil || *hits != 1 {
		t.Fatalf("the SSRF guard let a loopback address through: %v (hits %d)", err, *hits)
	}

	target, targetHits := serveTemplate(t, sampleTemplate)
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/other", http.StatusFound)
	}))
	t.Cleanup(redirect.Close)
	if _, err := loopbackFetcher(redirect.URL).Fetch(ctx, "AbCdEf123456"); err == nil || *targetHits != 0 {
		t.Fatalf("a redirect must be refused and not followed: %v (target hits %d)", err, *targetHits)
	}

	huge, _ := serveTemplate(t, `{"serialized_source_guild":{"name":"`+strings.Repeat("a", MaxTemplateBytes)+`"}}`)
	_, err = loopbackFetcher(huge.URL).Fetch(ctx, "AbCdEf123456")
	if api, isAPI := httpx.AsAPIError(err); !isAPI || api.Status != http.StatusRequestEntityTooLarge {
		t.Fatalf("size cap: %v", err)
	}

	missing := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(missing.Close)
	_, err = loopbackFetcher(missing.URL).Fetch(ctx, "AbCdEf123456")
	if api, isAPI := httpx.AsAPIError(err); !isAPI || api.Status != http.StatusNotFound {
		t.Fatalf("404: %v", err)
	}

	garbage, _ := serveTemplate(t, `{"hello": "world"}`)
	if _, err := loopbackFetcher(garbage.URL).Fetch(ctx, "AbCdEf123456"); err == nil {
		t.Fatal("a document that is not a template must be refused")
	}

	// The host is fixed: the code can never smuggle in another one.
	if _, err := loopbackFetcher(ok.URL).Fetch(ctx, "x/../../evil"); !errors.Is(err, errBadLink) {
		t.Fatalf("code with a path: %v", err)
	}
	if NewFetcher().base != "https://discord.com/api/v10/guilds/templates/" {
		t.Fatal("the production base URL must be fixed to discord.com")
	}
}
