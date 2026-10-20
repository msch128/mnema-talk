package bot

import (
	"strings"
	"testing"
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
