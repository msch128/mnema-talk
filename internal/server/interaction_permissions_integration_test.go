//go:build integration

package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/bot"
	"github.com/msch128/mnema-talk/internal/perm"
)

type interactionPayloadTest struct {
	ID        uuid.UUID `json:"id"`
	Type      int       `json:"type"`
	ChannelID uuid.UUID `json:"channel_id"`
	Member    struct {
		Permissions string `json:"permissions"`
		User        struct {
			ID uuid.UUID `json:"id"`
		} `json:"user"`
	} `json:"member"`
}

func TestInteractionMemberPermissionsBitfieldAndNoLeak(t *testing.T) {
	cf := newComponentsFixture(t)
	normal := cf.max

	// Register a simple command.
	cmdRes := cf.client.post(bot.BasePath+"/commands", map[string]any{
		"name":        "ping",
		"description": "Ping",
	})
	wantStatus(t, "register ping", cmdRes, http.StatusOK)
	var cmd bot.Command
	cmdRes.decode(t, &cmd)

	// Set up second channel with an overwrite denying SendMessages to normal members.
	chB := cf.a.createChannel(cf.owner, "channel-b", "text")
	roleMuted := createRole(t, cf.owner, "DenySend", 0)
	wantStatus(t, "overwrite deny send", putOverwrites(cf.owner, "channels", chB,
		overwrite("role", everyoneID(t, cf.owner), perm.ViewChannel, 0),
		overwrite("role", roleMuted, 0, perm.SendMessages),
		overwrite("role", cf.roleID, perm.ViewChannel|perm.SendMessages, 0),
	), http.StatusNoContent)
	assignRoles(t, cf.owner, normal, roleMuted)

	// Also connect a second bot to verify no leak to other bots.
	otherBot := createBot(t, cf.owner, "second-bot")
	otherTok := issueToken(t, cf.owner, otherBot.ID, map[string]any{})
	g2 := cf.dialGW("Bot " + otherTok.Secret)
	g2.identify("", 0)

	// 1. Normal member invokes in open channel (where Send is allowed).
	runRes := normal.post("/api/channels/"+cf.open.String()+"/interactions", map[string]any{
		"command_id": cmd.ID,
		"options":    map[string]any{},
	})
	wantStatus(t, "run in open", runRes, http.StatusAccepted)
	var invoked bot.Invoked
	runRes.decode(t, &invoked)
	if invoked.InteractionID == uuid.Nil {
		t.Fatal("expected interaction id in response")
	}

	_, d1 := cf.g.expect("INTERACTION_CREATE")
	g2.nothing() // Second bot must NOT receive anything.

	var p1 interactionPayloadTest
	if err := json.Unmarshal(d1, &p1); err != nil {
		t.Fatal(err)
	}
	bits1, err := strconv.ParseUint(p1.Member.Permissions, 10, 64)
	if err != nil {
		t.Fatalf("permissions not a decimal bitfield string: %s", p1.Member.Permissions)
	}
	if !perm.Set(bits1).Has(perm.SendMessages) {
		t.Fatalf("normal member in open channel should have SendMessages: %b", bits1)
	}

	// 2. Button click in channel B (where normal member is denied SendMessages).
	// Normal member cannot invoke commands (requires SendMessages), but can use components (only ViewChannel).
	msg := cf.postWithComponents(t, chB, "Click me", deployRows())
	clickRes := normal.post(clickPath(chB, msg.ID), map[string]any{"custom_id": "deploy:v2.4"})
	wantStatus(t, "click in channel B", clickRes, http.StatusAccepted)

	_, d2 := cf.g.expect("INTERACTION_CREATE")
	g2.nothing() // Second bot must NOT receive anything.

	var p2 interactionPayloadTest
	if err := json.Unmarshal(d2, &p2); err != nil {
		t.Fatal(err)
	}
	bits2, err := strconv.ParseUint(p2.Member.Permissions, 10, 64)
	if err != nil {
		t.Fatalf("permissions not a decimal bitfield string: %s", p2.Member.Permissions)
	}
	if perm.Set(bits2).Has(perm.SendMessages) {
		t.Fatalf("normal member in channel B must NOT have SendMessages: %b", bits2)
	}
	if bits1 == bits2 {
		t.Fatalf("permissions must differ between channels: %b vs %b", bits1, bits2)
	}

	// 3. Owner invokes command in channel B: Owner always has all bits.
	ownerRunRes := cf.owner.post("/api/channels/"+chB.String()+"/interactions", map[string]any{
		"command_id": cmd.ID,
		"options":    map[string]any{},
	})
	wantStatus(t, "owner run in channel B", ownerRunRes, http.StatusAccepted)

	_, d3 := cf.g.expect("INTERACTION_CREATE")
	g2.nothing()

	var p3 interactionPayloadTest
	if err := json.Unmarshal(d3, &p3); err != nil {
		t.Fatal(err)
	}
	wantOwner := strconv.FormatUint(uint64(perm.All), 10)
	if p3.Member.Permissions != wantOwner {
		t.Fatalf("owner permissions = %s, want all bits %s", p3.Member.Permissions, wantOwner)
	}
}

func TestCommandDefaultMemberPermissionsCRUD(t *testing.T) {
	f := gwFixture(t)

	// 1. Register with absent, null, "0", valid bits
	testCases := []struct {
		name string
		body map[string]any
		want *string
	}{
		{
			name: "absent",
			body: map[string]any{"name": "cmd-absent", "description": "d"},
			want: nil,
		},
		{
			name: "null",
			body: map[string]any{"name": "cmd-null", "description": "d", "default_member_permissions": nil},
			want: nil,
		},
		{
			name: "zero",
			body: map[string]any{"name": "cmd-zero", "description": "d", "default_member_permissions": "0"},
			want: strPtr("0"),
		},
		{
			name: "valid-ban",
			body: map[string]any{"name": "cmd-ban", "description": "d", "default_member_permissions": strconv.FormatUint(uint64(perm.BanMembers), 10)},
			want: strPtr(strconv.FormatUint(uint64(perm.BanMembers), 10)),
		},
	}

	for _, tc := range testCases {
		res := f.client.post(bot.BasePath+"/commands", tc.body)
		wantStatus(t, tc.name, res, http.StatusOK)
		var cmd bot.Command
		res.decode(t, &cmd)
		if tc.want == nil {
			if cmd.DefaultMemberPermissions != nil {
				t.Fatalf("%s: expected nil, got %v", tc.name, *cmd.DefaultMemberPermissions)
			}
		} else {
			if cmd.DefaultMemberPermissions == nil || *cmd.DefaultMemberPermissions != *tc.want {
				t.Fatalf("%s: expected %v, got %v", tc.name, *tc.want, cmd.DefaultMemberPermissions)
			}
		}
	}

	// 2. Invalid inputs rejected with 400
	for _, badPerm := range []any{"invalid", "-1", strconv.FormatUint(1<<60, 10), 123, true} {
		res := f.client.post(bot.BasePath+"/commands", map[string]any{
			"name":                       "cmd-bad",
			"description":                "d",
			"default_member_permissions": badPerm,
		})
		wantStatus(t, "invalid perm", res, http.StatusBadRequest)
	}

	// 3. Updating command updates default_member_permissions
	updateRes := f.client.post(bot.BasePath+"/commands", map[string]any{
		"name":                       "cmd-zero",
		"description":                "d",
		"default_member_permissions": strconv.FormatUint(uint64(perm.Administrator), 10),
	})
	wantStatus(t, "update command", updateRes, http.StatusOK)
	var updated bot.Command
	updateRes.decode(t, &updated)
	wantAdmin := strconv.FormatUint(uint64(perm.Administrator), 10)
	if updated.DefaultMemberPermissions == nil || *updated.DefaultMemberPermissions != wantAdmin {
		t.Fatalf("updated: expected %s, got %v", wantAdmin, updated.DefaultMemberPermissions)
	}

	// 4. Admin-disabled command stays disabled after update
	wantStatus(t, "disable", f.owner.patch(f.botPath+"/commands/"+updated.ID.String(), map[string]any{"disabled": true}), http.StatusOK)
	reRegRes := f.client.post(bot.BasePath+"/commands", map[string]any{
		"name":                       "cmd-zero",
		"description":                "new description",
		"default_member_permissions": "0",
	})
	wantStatus(t, "re-register disabled", reRegRes, http.StatusOK)
	var reReg bot.Command
	reRegRes.decode(t, &reReg)
	if !reReg.Disabled {
		t.Fatal("command should remain disabled after re-registration")
	}
	if reReg.DefaultMemberPermissions == nil || *reReg.DefaultMemberPermissions != "0" {
		t.Fatalf("expected updated permissions '0', got %v", reReg.DefaultMemberPermissions)
	}
}

func TestCommandDefaultMemberPermissionsEnforcement(t *testing.T) {
	cf := newComponentsFixture(t)
	normal := cf.max

	adminMember := cf.a.register(cf.owner, "admin_member")
	adminRole := createRole(t, cf.owner, "AdminRole", perm.Administrator)
	assignRoles(t, cf.owner, adminMember, adminRole)

	modMember := cf.a.register(cf.owner, "mod_member")
	banRole := createRole(t, cf.owner, "BanRole", perm.BanMembers)
	assignRoles(t, cf.owner, modMember, banRole)

	// Register commands:
	// - cmdOpen: no restrictions
	// - cmdZero: "0" (nobody except Owner/Admins)
	// - cmdBan: perm.BanMembers
	resOpen := cf.client.post(bot.BasePath+"/commands", map[string]any{"name": "cmd-open", "description": "Open"})
	var cmdOpen bot.Command
	resOpen.decode(t, &cmdOpen)

	resZero := cf.client.post(bot.BasePath+"/commands", map[string]any{"name": "cmd-zero", "description": "Zero", "default_member_permissions": "0"})
	var cmdZero bot.Command
	resZero.decode(t, &cmdZero)

	resBan := cf.client.post(bot.BasePath+"/commands", map[string]any{
		"name":                       "cmd-ban",
		"description":                "Ban",
		"default_member_permissions": strconv.FormatUint(uint64(perm.BanMembers), 10),
	})
	var cmdBan bot.Command
	resBan.decode(t, &cmdBan)

	hasName := func(cmds []bot.ChannelCommand, name string) bool {
		for _, c := range cmds {
			if c.Name == name {
				return true
			}
		}
		return false
	}

	// 1. Channel picker visibility
	normalList := channelCommands(t, normal, cf.open)
	if !hasName(normalList, "cmd-open") {
		t.Fatal("normal member should see cmd-open")
	}
	if hasName(normalList, "cmd-zero") {
		t.Fatal("normal member should NOT see cmd-zero")
	}
	if hasName(normalList, "cmd-ban") {
		t.Fatal("normal member should NOT see cmd-ban")
	}

	modList := channelCommands(t, modMember, cf.open)
	if !hasName(modList, "cmd-open") || !hasName(modList, "cmd-ban") {
		t.Fatal("mod member should see cmd-open and cmd-ban")
	}
	if hasName(modList, "cmd-zero") {
		t.Fatal("mod member should NOT see cmd-zero")
	}

	adminList := channelCommands(t, adminMember, cf.open)
	if !hasName(adminList, "cmd-open") || !hasName(adminList, "cmd-zero") || !hasName(adminList, "cmd-ban") {
		t.Fatal("admin member should see all commands")
	}

	ownerList := channelCommands(t, cf.owner, cf.open)
	if !hasName(ownerList, "cmd-open") || !hasName(ownerList, "cmd-zero") || !hasName(ownerList, "cmd-ban") {
		t.Fatal("owner should see all commands")
	}

	// 2. Invocation enforcement: 403 naming missing permission if lacking
	runCmd := func(user *client, id uuid.UUID) response {
		return user.post("/api/channels/"+cf.open.String()+"/interactions", map[string]any{
			"command_id": id,
			"options":    map[string]any{},
		})
	}

	// Normal member running cmd-zero -> 403 administrator
	resZ := runCmd(normal, cmdZero.ID)
	wantStatus(t, "normal run zero", resZ, http.StatusForbidden)
	if !strings.Contains(string(resZ.body), "administrator") {
		t.Fatalf("expected error naming administrator, got: %s", resZ.body)
	}

	// Normal member running cmd-ban -> 403 ban members
	resB := runCmd(normal, cmdBan.ID)
	wantStatus(t, "normal run ban", resB, http.StatusForbidden)
	if !strings.Contains(string(resB.body), "ban members") {
		t.Fatalf("expected error naming ban members, got: %s", resB.body)
	}

	// Mod member running cmd-ban -> 202 Accepted
	wantStatus(t, "mod run ban", runCmd(modMember, cmdBan.ID), http.StatusAccepted)

	// Admin member running cmd-zero and cmd-ban -> 202 Accepted
	wantStatus(t, "admin run zero", runCmd(adminMember, cmdZero.ID), http.StatusAccepted)
	wantStatus(t, "admin run ban", runCmd(adminMember, cmdBan.ID), http.StatusAccepted)

	// Owner running cmd-zero -> 202 Accepted
	wantStatus(t, "owner run zero", runCmd(cf.owner, cmdZero.ID), http.StatusAccepted)
}

func TestManifestInstallWithDefaultMemberPermissions(t *testing.T) {
	f := gwFixture(t)

	manifestDoc := map[string]any{
		"manifest_version": 1,
		"name":             "manifest-bot",
		"description":      "Installed from manifest",
		"permissions":      []string{"view_channel", "send_messages"},
		"commands": []map[string]any{
			{"name": "m-open", "description": "Open"},
			{"name": "m-zero", "description": "Zero", "default_member_permissions": "0"},
			{"name": "m-ban", "description": "Ban", "default_member_permissions": strconv.FormatUint(uint64(perm.BanMembers), 10)},
		},
	}

	installRes := f.owner.post("/api/admin/bots/install", map[string]any{
		"manifest":    manifestDoc,
		"permissions": []string{"view_channel", "send_messages"},
		"channel_ids": []string{f.open.String()},
	})
	wantStatus(t, "install bot", installRes, http.StatusCreated)
	var installResult struct {
		Bot struct {
			ID uuid.UUID `json:"id"`
		} `json:"bot"`
	}
	installRes.decode(t, &installResult)

	// Check commands via admin listing
	cmdRes := f.owner.get("/api/admin/bots/" + installResult.Bot.ID.String() + "/commands")
	wantStatus(t, "list installed commands", cmdRes, http.StatusOK)
	var cmds []bot.Command
	cmdRes.decode(t, &cmds)
	if len(cmds) != 3 {
		t.Fatalf("expected 3 commands, got %d", len(cmds))
	}

	cmdMap := map[string]bot.Command{}
	for _, c := range cmds {
		cmdMap[c.Name] = c
	}

	if cmdMap["m-open"].DefaultMemberPermissions != nil {
		t.Fatalf("m-open: expected nil, got %v", *cmdMap["m-open"].DefaultMemberPermissions)
	}
	if cmdMap["m-zero"].DefaultMemberPermissions == nil || *cmdMap["m-zero"].DefaultMemberPermissions != "0" {
		t.Fatalf("m-zero: expected '0', got %v", cmdMap["m-zero"].DefaultMemberPermissions)
	}
	wantBan := strconv.FormatUint(uint64(perm.BanMembers), 10)
	if cmdMap["m-ban"].DefaultMemberPermissions == nil || *cmdMap["m-ban"].DefaultMemberPermissions != wantBan {
		t.Fatalf("m-ban: expected %s, got %v", wantBan, cmdMap["m-ban"].DefaultMemberPermissions)
	}
}
