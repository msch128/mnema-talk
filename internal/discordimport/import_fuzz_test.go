package discordimport

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/msch128/mnema-talk/internal/perm"
)

// FuzzParseCode checks a pasted link or code is either refused or reduced to a
// plain code of the allowed alphabet, and that only Discord hosts are taken.
func FuzzParseCode(f *testing.F) {
	for _, s := range []string{
		"AbCdEf123456", "https://discord.new/AbCdEf123456", "discord.com/template/AbCdEf123456/", "http://discord.com/template/x?y=1",
		"https://evil.example/template/AbCdEf", "https://discord.com@evil.example/template/AbCdEf", "https://discord.new\\@evil.example/abc",
		"HTTPS://DISCORD.NEW/abc", "a", "", "https://discord.new/", "discord.new/../abc", "\x00",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, input string) {
		code, err := ParseCode(input)
		if err != nil {
			return
		}
		if !codePattern.MatchString(code) {
			t.Fatalf("ParseCode(%q) = %q", input, code)
		}
		if !codePattern.MatchString(strings.TrimSpace(input)) && !strings.Contains(strings.ToLower(input), "discord") {
			t.Fatalf("ParseCode(%q) accepted a link that names no Discord host", input)
		}
	})
}

// FuzzBuildPlan checks that any template document that decodes produces a plan
// that never panics, never carries Administrator or an owner-only power, only
// creates (the counts match the lists), keeps names within their limits and is
// the same plan every time (its hash is what the administrator confirms).
func FuzzBuildPlan(f *testing.F) {
	f.Add(sampleTemplate)
	f.Add(`{"serialized_source_guild":{"roles":[{"id":"1","name":"a","permissions":"18446744073709551615"}],"channels":[]}}`)
	f.Add(`{"serialized_source_guild":{"channels":[{"id":1,"type":0,"name":"x","parent_id":1,"rate_limit_per_user":-5},{"id":1,"type":4,"name":"x"}]}}`)
	f.Add(`{"serialized_source_guild":{"roles":[{"id":0,"name":"@everyone","permissions":"8"}],"channels":[{"id":2,"type":0,"name":"p","permission_overwrites":[{"id":0,"type":"role","allow":"8","deny":"1024"}]}]}}`)
	f.Fuzz(func(t *testing.T, doc string) {
		if len(doc) > MaxTemplateBytes {
			return
		}
		var tpl Template
		if json.Unmarshal([]byte(doc), &tpl) != nil || len(tpl.Guild.Channels) > maxTemplateChannels || len(tpl.Guild.Roles) > maxTemplateRoles {
			return
		}
		in := Inputs{Grantable: grantable(^perm.Set(0)), ExistingRoles: []string{"Admin"}, Caps: Capabilities{Forum: true, Stage: true}}
		plan := BuildPlan(&tpl, in)
		if again := BuildPlan(&tpl, in); again.Hash != plan.Hash || plan.Hash == "" {
			t.Fatal("the plan is not deterministic")
		}
		if plan.Counts.Roles != len(plan.Roles) || plan.Counts.Channels != len(plan.Channels) || plan.Counts.Categories != len(plan.Categories) {
			t.Fatal("counts do not match the lists")
		}
		forbidden := perm.Administrator | perm.OwnerGrantOnly
		for _, r := range plan.Roles {
			if r.Permissions&forbidden != 0 || utf8.RuneCountInString(r.Name) > maxRoleName || r.Color < 0 || r.Color > 0xffffff {
				t.Fatalf("role %+v is out of bounds", r)
			}
		}
		for _, c := range plan.Channels {
			for _, o := range c.Overwrites {
				if (o.Allow|o.Deny)&forbidden != 0 {
					t.Fatalf("channel %q overwrite carries a forbidden power", c.Name)
				}
			}
		}
	})
}
