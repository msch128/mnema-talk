package bot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

func validManifest() Manifest {
	return Manifest{
		ManifestVersion: 1, Name: "Mnemo", Description: "Answers questions.",
		Homepage: "https://example.com/mnemo",
		Intents:  []string{"GUILD_MESSAGES", "GUILDS"}, Permissions: []string{"send_messages", "view_channel"},
		Commands: []CommandInput{{Name: "ask", Description: "Ask something"}},
	}
}

func commands(n int) []CommandInput {
	out := make([]CommandInput, n)
	for i := range out {
		out[i] = CommandInput{Name: fmt.Sprintf("c%d", i), Description: "d"}
	}
	return out
}

func TestManifestCheckRefusesWhatAManifestCannotSay(t *testing.T) {
	cases := map[string]struct {
		change func(*Manifest)
		want   string
	}{
		"no version":                {func(m *Manifest) { m.ManifestVersion = 0 }, "manifest_version"},
		"a newer version":           {func(m *Manifest) { m.ManifestVersion = 2 }, "manifest_version"},
		"no name":                   {func(m *Manifest) { m.Name = "  " }, "name"},
		"a long name":               {func(m *Manifest) { m.Name = strings.Repeat("x", 25) }, "name"},
		"a long description":        {func(m *Manifest) { m.Description = strings.Repeat("é", 201) }, "description"},
		"a plain http homepage":     {func(m *Manifest) { m.Homepage = "http://example.com" }, "homepage"},
		"a javascript homepage":     {func(m *Manifest) { m.Homepage = "javascript:alert(1)" }, "homepage"},
		"a homepage without a host": {func(m *Manifest) { m.Homepage = "https:///x" }, "homepage"},
		"a homepage with a login":   {func(m *Manifest) { m.Homepage = "https://user:pw@example.com" }, "homepage"},
		"a long homepage":           {func(m *Manifest) { m.Homepage = "https://example.com/" + strings.Repeat("a", 500) }, "homepage"},
		"an unknown intent":         {func(m *Manifest) { m.Intents = []string{"GUILD_VOICE_STATES"} }, "GUILD_VOICE_STATES"},
		"administrator":             {func(m *Manifest) { m.Permissions = []string{"administrator"} }, "administrator"},
		"an admin-area permission":  {func(m *Manifest) { m.Permissions = []string{"view_channel", "manage_bots"} }, "manage_bots"},
		"a voice permission":        {func(m *Manifest) { m.Permissions = []string{"connect"} }, "connect"},
		"an elevated channel bit":   {func(m *Manifest) { m.Permissions = []string{"disconnect_members"} }, "disconnect_members"},
		"an unknown permission":     {func(m *Manifest) { m.Permissions = []string{"rule_the_world"} }, "rule_the_world"},
		"a bad command name":        {func(m *Manifest) { m.Commands = []CommandInput{{Name: "Ask", Description: "d"}} }, "command names"},
		"a bad command permission": {func(m *Manifest) {
			s := "invalid"
			m.Commands = []CommandInput{{Name: "ask", Description: "d", DefaultMemberPermissions: &s}}
		}, "unknown permission bits"},
		"a command twice":   {func(m *Manifest) { m.Commands = append(commands(1), commands(1)...) }, "duplicate"},
		"too many commands": {func(m *Manifest) { m.Commands = commands(MaxCommands + 1) }, "at most 50"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			m := validManifest()
			tc.change(&m)
			_, err := m.Check()
			ae, ok := httpx.AsAPIError(err)
			if !ok || ae.Status != http.StatusBadRequest || !strings.Contains(ae.Message, tc.want) {
				t.Fatalf("got %v, want a 400 naming %q", err, tc.want)
			}
		})
	}
}

func TestManifestCheckNormalises(t *testing.T) {
	m := validManifest()
	m.Name = "  Mnemo  "
	m.Description = "  Answers questions.  "
	m.Intents = append(m.Intents, "GUILDS")
	m.Permissions = append(m.Permissions, "view_channel", "mention_everyone")
	m.Commands = commands(MaxCommands)
	got, err := m.Check()
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Mnemo" || got.Description != "Answers questions." || got.Homepage != "https://example.com/mnemo" {
		t.Fatalf("text: %+v", got)
	}
	wantIntents := []ManifestIntent{{"GUILDS", IntentGuilds}, {"GUILD_MESSAGES", IntentGuildMessages}}
	if fmt.Sprint(got.Intents) != fmt.Sprint(wantIntents) {
		t.Fatalf("intents in bit order without duplicates: %+v", got.Intents)
	}
	var keys []string
	for _, p := range got.Permissions {
		keys = append(keys, p.Key)
	}
	if strings.Join(keys, ",") != "view_channel,send_messages,mention_everyone" || got.Permissions[0].Name != "view channels" || got.Permissions[0].Bit != perm.ViewChannel {
		t.Fatalf("permissions in catalog order with names: %+v", got.Permissions)
	}
	if got.bits() != perm.ViewChannel|perm.SendMessages|perm.MentionEveryone || got.intentBits() != IntentGuilds|IntentGuildMessages {
		t.Fatalf("bits: %b %b", got.bits(), got.intentBits())
	}
	if len(got.Commands) != MaxCommands || got.Commands[0].Options == nil {
		t.Fatalf("commands: %d", len(got.Commands))
	}
}

func TestManifestCheckAcceptsTheMinimum(t *testing.T) {
	got, err := Manifest{ManifestVersion: 1, Name: "x"}.Check()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(got)
	if !strings.Contains(string(raw), `"intents":[]`) || !strings.Contains(string(raw), `"permissions":[]`) || !strings.Contains(string(raw), `"commands":[]`) {
		t.Fatalf("empty lists stay lists: %s", raw)
	}
}

func decodeBody(t *testing.T, body string, dst any) error {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
	return httpx.DecodeJSON(r, dst)
}

func TestManifestIgnoresUnknownFieldsAtEveryLevel(t *testing.T) {
	body := `{"manifest_version": 1, "name": "x", "future": {"a": 1}, "avatar": "data:image/png;base64,AAAA",
		"commands": [{"name": "ask", "description": "d", "localizations": {}, "options": [{"name": "q", "description": "d", "type": 3, "autocomplete": true}]}]}`
	var m Manifest
	if err := decodeBody(t, body, &m); err != nil {
		t.Fatalf("a manifest with unknown fields: %v", err)
	}
	if _, err := m.Check(); err != nil {
		t.Fatal(err)
	}
	var req InstallRequest
	if err := decodeBody(t, `{"manifest": `+body+`, "permissions": [], "intents": [], "channel_ids": []}`, &req); err != nil {
		t.Fatalf("an install with such a manifest: %v", err)
	}
	if req.Manifest.Name != "x" || len(req.Manifest.Commands) != 1 {
		t.Fatalf("decoded: %+v", req.Manifest)
	}
	// The request around the manifest stays strict, and known fields keep their types.
	if err := decodeBody(t, `{"manifest": {"manifest_version": 1, "name": "x"}, "extra": 1}`, &req); err == nil {
		t.Fatal("an unknown field next to the manifest was accepted")
	}
	if err := decodeBody(t, `{"manifest_version": "1", "name": "x"}`, &m); err == nil {
		t.Fatal("a version as a string was accepted")
	}
	if err := decodeBody(t, `{"manifest_version": 1, "name": "x", "avatar": {"url": "https://example.com/a.png"}}`, &m); err == nil {
		t.Fatal("an avatar that is not a string was accepted")
	}
}

func TestInstallPlanKeepsOnlyWhatTheManifestAsks(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	ok := InstallRequest{Manifest: validManifest(), Permissions: []string{"view_channel"}, Intents: []string{"GUILDS"}, ChannelIDs: []uuid.UUID{a, b, a}}
	p, err := planInstall(ok)
	if err != nil {
		t.Fatal(err)
	}
	if p.perms != perm.ViewChannel || p.intents != IntentGuilds || len(p.channels) != 2 {
		t.Fatalf("plan: %+v", p)
	}
	for name, req := range map[string]InstallRequest{
		"a permission the manifest does not ask for": {Manifest: validManifest(), Permissions: []string{"add_reactions"}},
		"an intent the manifest does not ask for":    {Manifest: validManifest(), Intents: []string{"GUILD_MEMBERS"}},
		"an elevated permission":                     {Manifest: validManifest(), Permissions: []string{"administrator"}},
		"too many channels":                          {Manifest: validManifest(), ChannelIDs: manyIDs(maxInstallChannels + 1)},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := planInstall(req); err == nil {
				t.Fatal("accepted")
			} else if ae, ok := httpx.AsAPIError(err); !ok || ae.Status != http.StatusBadRequest {
				t.Fatalf("got %v, want 400", err)
			}
		})
	}
}

func manyIDs(n int) []uuid.UUID {
	out := make([]uuid.UUID, n)
	for i := range out {
		out[i] = uuid.New()
	}
	return out
}

func TestManifestDefaultMemberPermissions(t *testing.T) {
	str := func(s string) *string { return &s }
	m := validManifest()
	m.Commands = []CommandInput{
		{Name: "c1", Description: "d", DefaultMemberPermissions: str("0")},
		{Name: "c2", Description: "d", DefaultMemberPermissions: str("8")},
		{Name: "c3", Description: "d", DefaultMemberPermissions: nil},
	}
	checked, err := m.Check()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(checked.Commands) != 3 {
		t.Fatalf("expected 3 commands, got %d", len(checked.Commands))
	}
	if checked.Commands[0].DefaultMemberPermissions == nil || *checked.Commands[0].DefaultMemberPermissions != "0" {
		t.Fatalf("c1: expected '0', got %v", checked.Commands[0].DefaultMemberPermissions)
	}
	if checked.Commands[1].DefaultMemberPermissions == nil || *checked.Commands[1].DefaultMemberPermissions != "8" {
		t.Fatalf("c2: expected '8', got %v", checked.Commands[1].DefaultMemberPermissions)
	}
	if checked.Commands[2].DefaultMemberPermissions != nil {
		t.Fatalf("c3: expected nil, got %v", *checked.Commands[2].DefaultMemberPermissions)
	}
}
