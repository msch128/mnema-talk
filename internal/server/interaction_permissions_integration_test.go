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
	wantBits1 := channelPerms(myPerms(t, normal), cf.open)
	if perm.Set(bits1) != wantBits1 {
		t.Fatalf("normal member in open channel got %d, want exact set %d", bits1, wantBits1)
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
	wantBits2 := channelPerms(myPerms(t, normal), chB)
	if perm.Set(bits2) != wantBits2 {
		t.Fatalf("normal member in channel B got %d, want exact set %d", bits2, wantBits2)
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

	// 4. Administrator member invokes command in channel B: receives perm.All.
	adminMember := cf.a.register(cf.owner, "admin_interaction_user")
	adminRole := createRole(t, cf.owner, "AdminInteractionRole", perm.Administrator)
	assignRoles(t, cf.owner, adminMember, adminRole)
	adminRunRes := adminMember.post("/api/channels/"+chB.String()+"/interactions", map[string]any{
		"command_id": cmd.ID,
		"options":    map[string]any{},
	})
	wantStatus(t, "admin run in channel B", adminRunRes, http.StatusAccepted)

	_, dAdmin := cf.g.expect("INTERACTION_CREATE")
	g2.nothing()

	var pAdmin interactionPayloadTest
	if err := json.Unmarshal(dAdmin, &pAdmin); err != nil {
		t.Fatal(err)
	}
	if pAdmin.Member.Permissions != wantOwner {
		t.Fatalf("admin member permissions = %s, want all bits %s", pAdmin.Member.Permissions, wantOwner)
	}
}

func TestCommandDefaultMemberPermissionsCRUD(t *testing.T) {
	f := gwFixture(t)

	// 1. Register with absent, null, "0", valid bits, and array forms
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
		{
			name: "array-empty",
			body: map[string]any{"name": "cmd-arr-empty", "description": "d", "default_member_permissions": []string{}},
			want: strPtr("0"),
		},
		{
			name: "array-ban",
			body: map[string]any{"name": "cmd-arr-ban", "description": "d", "default_member_permissions": []string{"ban_members"}},
			want: strPtr(strconv.FormatUint(uint64(perm.BanMembers), 10)),
		},
		{
			name: "array-multi",
			body: map[string]any{"name": "cmd-arr-multi", "description": "d", "default_member_permissions": []string{"view_channel", "send_messages"}},
			want: strPtr("6"),
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

	// 2. Invalid inputs rejected with 400 (bits, arrays, unknown keys, mix)
	for _, badPerm := range []any{
		"invalid", "-1", strconv.FormatUint(1<<60, 10), 123, true,
		[]string{"unknown_perm"},
		[]string{"ban_members", "unknown_perm_key"},
		[]any{"ban_members", 123},
	} {
		res := f.client.post(bot.BasePath+"/commands", map[string]any{
			"name":                       "cmd-bad",
			"description":                "d",
			"default_member_permissions": badPerm,
		})
		wantStatus(t, "invalid perm", res, http.StatusBadRequest)
	}

	resUnknown := f.client.post(bot.BasePath+"/commands", map[string]any{
		"name":                       "cmd-bad-key",
		"description":                "d",
		"default_member_permissions": []string{"unknown_perm_key"},
	})
	wantStatus(t, "unknown key", resUnknown, http.StatusBadRequest)
	if !strings.Contains(string(resUnknown.body), "unknown_perm_key") {
		t.Fatalf("expected error naming unknown_perm_key, got: %s", resUnknown.body)
	}

	resMix := f.client.post(bot.BasePath+"/commands", map[string]any{
		"name":                       "cmd-bad-mix",
		"description":                "d",
		"default_member_permissions": []string{"ban_members", "bad_mix_key"},
	})
	wantStatus(t, "mix key", resMix, http.StatusBadRequest)
	if !strings.Contains(string(resMix.body), "bad_mix_key") {
		t.Fatalf("expected error naming bad_mix_key, got: %s", resMix.body)
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

	// 3. Re-registering without default_member_permissions clears it to null (everyone)
	reRegOpen := cf.client.post(bot.BasePath+"/commands", map[string]any{
		"name":        "cmd-ban",
		"description": "Ban cleared",
	})
	wantStatus(t, "re-register cleared", reRegOpen, http.StatusOK)
	var clearedCmd bot.Command
	reRegOpen.decode(t, &clearedCmd)
	if clearedCmd.DefaultMemberPermissions != nil {
		t.Fatalf("expected nil permissions after clearing, got %v", *clearedCmd.DefaultMemberPermissions)
	}

	normalListAfter := channelCommands(t, normal, cf.open)
	if !hasName(normalListAfter, "cmd-ban") {
		t.Fatal("normal member should see cmd-ban after re-registering without field")
	}
	wantStatus(t, "normal run cleared ban", runCmd(normal, cmdBan.ID), http.StatusAccepted)
}

func TestInteractionMemberPermissionsNoteAndModal(t *testing.T) {
	cf := newComponentsFixture(t)
	normal := cf.max
	in := cf.command(t)

	// 1. Note component interaction carries member.permissions
	againRow := []any{
		map[string]any{
			"type": 1,
			"components": []any{
				map[string]any{"type": 2, "custom_id": "again", "label": "Again", "style": 1},
			},
		},
	}
	wantStatus(t, "ephemeral reply", cf.callback(in, 4, map[string]any{
		"content": "Ephemeral note", "flags": 64, "components": againRow,
	}), http.StatusNoContent)
	note := expectNote(t, cf.maxWS)

	noteClickRes := normal.post("/api/channels/"+cf.open.String()+"/ephemeral/"+note.ID.String()+"/components", map[string]any{
		"custom_id": "again",
	})
	wantStatus(t, "click note component", noteClickRes, http.StatusAccepted)
	_, dNote := cf.g.expect("INTERACTION_CREATE")
	var pNote interactionPayloadTest
	if err := json.Unmarshal(dNote, &pNote); err != nil {
		t.Fatal(err)
	}
	wantPerms := strconv.FormatUint(uint64(channelPerms(myPerms(t, normal), cf.open)), 10)
	if pNote.Member.Permissions != wantPerms {
		t.Fatalf("note component permissions = %s, want %s", pNote.Member.Permissions, wantPerms)
	}

	// 2. Modal submit interaction carries member.permissions
	inModal := cf.command(t)
	wantStatus(t, "open modal", cf.callback(inModal, 9, map[string]any{
		"custom_id": "test_modal",
		"title":     "Test Modal",
		"components": []any{
			map[string]any{
				"type": 1,
				"components": []any{
					map[string]any{"type": 4, "custom_id": "field1", "label": "Input", "style": 1},
				},
			},
		},
	}), http.StatusNoContent)
	modal := expectModal(t, cf.maxWS)

	modalSubRes := normal.post("/api/channels/"+cf.open.String()+"/modals/"+modal.ID.String(), map[string]any{
		"values": map[string]string{"field1": "response"},
	})
	wantStatus(t, "submit modal", modalSubRes, http.StatusAccepted)
	_, dModal := cf.g.expect("INTERACTION_CREATE")
	var pModal interactionPayloadTest
	if err := json.Unmarshal(dModal, &pModal); err != nil {
		t.Fatal(err)
	}
	if pModal.Member.Permissions != wantPerms {
		t.Fatalf("modal submit permissions = %s, want %s", pModal.Member.Permissions, wantPerms)
	}
}

func TestCommandChannelScopedPermissionEnforcement(t *testing.T) {
	cf := newComponentsFixture(t)
	scopedUser := cf.a.register(cf.owner, "scoped_user")

	chA := cf.open
	chB := cf.a.createChannel(cf.owner, "channel-b-scoped", "text")

	// Create a role with 0 server-wide permissions.
	roleA := createRole(t, cf.owner, "PermInAOnly", 0)
	assignRoles(t, cf.owner, scopedUser, roleA)

	// In Channel A, overwrite allows ManageMessages to roleA.
	wantStatus(t, "overwrite allow manage_messages in chA", putOverwrites(cf.owner, "channels", chA,
		overwrite("role", everyoneID(t, cf.owner), perm.ViewChannel|perm.SendMessages, 0),
		overwrite("role", roleA, perm.ManageMessages, 0),
		overwrite("role", cf.roleID, perm.ViewChannel|perm.SendMessages, 0),
	), http.StatusNoContent)

	// In Channel B, roleA does not have ManageMessages (everyone gets View and Send).
	wantStatus(t, "overwrite in chB", putOverwrites(cf.owner, "channels", chB,
		overwrite("role", everyoneID(t, cf.owner), perm.ViewChannel|perm.SendMessages, 0),
		overwrite("role", cf.roleID, perm.ViewChannel|perm.SendMessages, 0),
	), http.StatusNoContent)

	// Register command requiring manage_messages (via array form).
	resCmd := cf.client.post(bot.BasePath+"/commands", map[string]any{
		"name":                       "cmd-channel-scoped",
		"description":                "Channel scoped",
		"default_member_permissions": []string{"manage_messages"},
	})
	wantStatus(t, "register channel scoped cmd", resCmd, http.StatusOK)
	var cmdScoped bot.Command
	resCmd.decode(t, &cmdScoped)

	hasName := func(cmds []bot.ChannelCommand, name string) bool {
		for _, c := range cmds {
			if c.Name == name {
				return true
			}
		}
		return false
	}

	// In Channel A: picker shows command, invoke succeeds (202).
	listA := channelCommands(t, scopedUser, chA)
	if !hasName(listA, "cmd-channel-scoped") {
		t.Fatal("scopedUser should see cmd-channel-scoped in channel A")
	}
	resRunA := scopedUser.post("/api/channels/"+chA.String()+"/interactions", map[string]any{
		"command_id": cmdScoped.ID,
		"options":    map[string]any{},
	})
	wantStatus(t, "run in channel A", resRunA, http.StatusAccepted)

	// In Channel B: picker hides command, invoke fails (403 naming manage messages).
	listB := channelCommands(t, scopedUser, chB)
	if hasName(listB, "cmd-channel-scoped") {
		t.Fatal("scopedUser should NOT see cmd-channel-scoped in channel B")
	}
	resRunB := scopedUser.post("/api/channels/"+chB.String()+"/interactions", map[string]any{
		"command_id": cmdScoped.ID,
		"options":    map[string]any{},
	})
	wantStatus(t, "run in channel B", resRunB, http.StatusForbidden)
	if !strings.Contains(string(resRunB.body), "manage messages") {
		t.Fatalf("expected 403 naming manage messages, got: %s", resRunB.body)
	}
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
			{"name": "m-arr", "description": "Arr", "default_member_permissions": []string{"view_channel", "send_messages"}},
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
	if len(cmds) != 4 {
		t.Fatalf("expected 4 commands, got %d", len(cmds))
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
	if cmdMap["m-arr"].DefaultMemberPermissions == nil || *cmdMap["m-arr"].DefaultMemberPermissions != "6" {
		t.Fatalf("m-arr: expected '6', got %v", cmdMap["m-arr"].DefaultMemberPermissions)
	}
}
