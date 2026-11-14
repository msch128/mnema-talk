package bot

import (
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/perm/permtest"
)

func TestCommandInputClean(t *testing.T) {
	// Valid command input
	valid := CommandInput{
		Name:        "test-cmd",
		Description: "A valid test command",
		Options: []Option{
			{Name: "text", Description: "some text", Type: OptionString, Required: true},
			{Name: "count", Description: "some number", Type: OptionInteger, Required: false},
		},
	}
	cleaned, err := valid.Clean()
	if err != nil {
		t.Fatalf("expected valid command to clean without error, got: %v", err)
	}
	if cleaned.Name != "test-cmd" || len(cleaned.Options) != 2 {
		t.Errorf("unexpected cleaned command: %+v", cleaned)
	}

	// Invalid command name
	invalidName := CommandInput{Name: "INVALID NAME!", Description: "bad"}
	if _, err := invalidName.Clean(); err == nil {
		t.Errorf("expected error for uppercase and spaces in command name")
	}

	// Too many options
	tooManyOpts := CommandInput{
		Name:        "overloaded",
		Description: "too many options",
		Options:     make([]Option, MaxOptions+1),
	}
	if _, err := tooManyOpts.Clean(); err == nil {
		t.Errorf("expected error for exceeding MaxOptions")
	}

	// Required option after optional option
	misorderedOpts := CommandInput{
		Name:        "misordered",
		Description: "bad option ordering",
		Options: []Option{
			{Name: "opt1", Description: "optional", Type: OptionString, Required: false},
			{Name: "opt2", Description: "required", Type: OptionString, Required: true},
		},
	}
	if _, err := misorderedOpts.Clean(); err == nil {
		t.Errorf("expected error for required option after optional option")
	}

	// Unknown option type
	badType := CommandInput{
		Name:        "bad-type",
		Description: "unknown type",
		Options: []Option{
			{Name: "opt", Description: "bad type", Type: 999, Required: false},
		},
	}
	if _, err := badType.Clean(); err == nil {
		t.Errorf("expected error for unknown option type")
	}

	// Duplicate option name
	dupOpts := CommandInput{
		Name:        "dup-opts",
		Description: "duplicate option names",
		Options: []Option{
			{Name: "same", Description: "first", Type: OptionString, Required: false},
			{Name: "same", Description: "second", Type: OptionString, Required: false},
		},
	}
	if _, err := dupOpts.Clean(); err == nil {
		t.Errorf("expected error for duplicate option names")
	}
}

func TestCollectUniqueCommandNames(t *testing.T) {
	// Exceeding MaxCommands
	tooMany := make([]CommandInput, MaxCommands+1)
	if _, err := collectUniqueCommandNames(tooMany); err == nil {
		t.Errorf("expected error when exceeding MaxCommands")
	}

	// Duplicate command names
	dups := []CommandInput{
		{Name: "ping"},
		{Name: "pong"},
		{Name: "ping"},
	}
	if _, err := collectUniqueCommandNames(dups); err == nil || !strings.Contains(err.Error(), "duplicate command name") {
		t.Errorf("expected duplicate command name error, got: %v", err)
	}

	// Unique names
	valid := []CommandInput{
		{Name: "ping"},
		{Name: "pong"},
	}
	names, err := collectUniqueCommandNames(valid)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(names) != 2 || names[0] != "ping" || names[1] != "pong" {
		t.Errorf("unexpected names: %v", names)
	}
}

func TestCommandInputDefaultMemberPermissions(t *testing.T) {
	str := func(s string) *string { return &s }

	// Valid inputs: absent, null, "0", valid bits, and leading zeroes
	for name, tc := range map[string]struct {
		input *string
		want  *string
	}{
		"absent/nil":     {input: nil, want: nil},
		"zero":           {input: str("0"), want: str("0")},
		"valid bits":     {input: str(strconv.FormatUint(uint64(perm.SendMessages|perm.AttachFiles), 10)), want: str(strconv.FormatUint(uint64(perm.SendMessages|perm.AttachFiles), 10))},
		"administrator":  {input: str(strconv.FormatUint(uint64(perm.Administrator), 10)), want: str(strconv.FormatUint(uint64(perm.Administrator), 10))},
		"all bits":       {input: str(strconv.FormatUint(uint64(perm.All), 10)), want: str(strconv.FormatUint(uint64(perm.All), 10))},
		"leading zeroes": {input: str("008"), want: str("8")},
	} {
		t.Run(name, func(t *testing.T) {
			in := CommandInput{Name: "cmd", Description: "desc", DefaultMemberPermissions: tc.input}
			cleaned, err := in.Clean()
			if err != nil {
				t.Fatalf("unexpected error for %s: %v", name, err)
			}
			if tc.want == nil {
				if cleaned.DefaultMemberPermissions != nil {
					t.Fatalf("want nil, got %v", *cleaned.DefaultMemberPermissions)
				}
			} else {
				if cleaned.DefaultMemberPermissions == nil || *cleaned.DefaultMemberPermissions != *tc.want {
					t.Fatalf("want %v, got %v", *tc.want, cleaned.DefaultMemberPermissions)
				}
			}
		})
	}

	// Invalid inputs
	for name, input := range map[string]string{
		"empty":        "",
		"letters":      "invalid",
		"negative":     "-1",
		"hex":          "0x10",
		"with spaces":  " 8 ",
		"unknown bits": strconv.FormatUint(1<<60, 10),
	} {
		t.Run(name, func(t *testing.T) {
			in := CommandInput{Name: "cmd", Description: "desc", DefaultMemberPermissions: str(input)}
			if _, err := in.Clean(); err == nil {
				t.Fatalf("expected error for %s", name)
			}
		})
	}
}

func TestCommandPermChecks(t *testing.T) {
	str := func(s string) *string { return &s }
	f := permtest.NewFixture()
	ch := f.Channel(uuid.Nil)
	deniedCh := f.Channel(uuid.Nil)

	ownerID := uuid.New()
	adminID := uuid.New()
	memberID := uuid.New()

	adminRole := f.Role(5, perm.Administrator)
	f.Assign(adminID, adminRole)

	// Deny SendMessages in deniedCh for @everyone
	f.RoleOverwrite(true, deniedCh, f.EveryoneID(), 0, perm.SendMessages)

	snap := f.Snapshot()
	ownerSub := perm.Subject{ID: ownerID, Owner: true}
	adminSub := perm.Subject{ID: adminID}
	memberSub := perm.Subject{ID: memberID}

	// 1. Nil default_member_permissions allows everyone
	if !hasCommandPerm(snap, memberSub, snap.Channel(memberSub, ch), nil) {
		t.Fatal("expected nil permissions to be allowed in picker")
	}
	if err := checkCommandPerm(snap, memberSub, snap.Channel(memberSub, ch), nil); err != nil {
		t.Fatalf("expected nil permissions to pass checkCommandPerm: %v", err)
	}

	// 2. "0" permissions: Owner and Administrator pass; normal member fails
	zero := str("0")
	if !hasCommandPerm(snap, ownerSub, snap.Channel(ownerSub, ch), zero) || !hasCommandPerm(snap, adminSub, snap.Channel(adminSub, ch), zero) {
		t.Fatal("expected owner and admin to have command perm for 0")
	}
	if hasCommandPerm(snap, memberSub, snap.Channel(memberSub, ch), zero) {
		t.Fatal("expected normal member to be hidden from picker for 0")
	}
	if err := checkCommandPerm(snap, ownerSub, snap.Channel(ownerSub, ch), zero); err != nil {
		t.Fatalf("expected owner to pass checkCommandPerm: %v", err)
	}
	if err := checkCommandPerm(snap, adminSub, snap.Channel(adminSub, ch), zero); err != nil {
		t.Fatalf("expected admin to pass checkCommandPerm: %v", err)
	}
	err := checkCommandPerm(snap, memberSub, snap.Channel(memberSub, ch), zero)
	if err == nil || !strings.Contains(err.Error(), "missing permission: administrator") {
		t.Fatalf("expected missing permission: administrator, got %v", err)
	}

	// 3. Permission checks differ by channel overwrite
	sendPerm := str(strconv.FormatUint(uint64(perm.SendMessages), 10))
	if !hasCommandPerm(snap, memberSub, snap.Channel(memberSub, ch), sendPerm) {
		t.Fatal("expected member to have send permission in open channel")
	}
	if err := checkCommandPerm(snap, memberSub, snap.Channel(memberSub, ch), sendPerm); err != nil {
		t.Fatalf("expected member to pass in open channel: %v", err)
	}
	if hasCommandPerm(snap, memberSub, snap.Channel(memberSub, deniedCh), sendPerm) {
		t.Fatal("expected member to be hidden in channel where Send is denied")
	}
	err = checkCommandPerm(snap, memberSub, snap.Channel(memberSub, deniedCh), sendPerm)
	if err == nil || !strings.Contains(err.Error(), "missing permission: send messages") {
		t.Fatalf("expected missing permission: send messages, got %v", err)
	}
	// Owner still passes in denied channel
	if !hasCommandPerm(snap, ownerSub, snap.Channel(ownerSub, deniedCh), sendPerm) {
		t.Fatal("owner must pass in denied channel")
	}
	if err := checkCommandPerm(snap, ownerSub, snap.Channel(ownerSub, deniedCh), sendPerm); err != nil {
		t.Fatalf("owner must pass checkCommandPerm in denied channel: %v", err)
	}
}
