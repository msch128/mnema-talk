package slash

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
)

type fake struct {
	def       Definition
	available bool
}

func (f fake) Definition() Definition                                 { return f.def }
func (f fake) Available(*perm.Snapshot, perm.Subject, uuid.UUID) bool { return f.available }
func (f fake) Run(context.Context, Call) (Result, error)              { return Result{}, nil }

func cmd(name string, available bool) fake {
	return fake{def: Definition{Name: name, Description: "does " + name}, available: available}
}

func TestIDsAreStablePerNameAndDifferBetweenNames(t *testing.T) {
	if ID("poll") != ID("poll") {
		t.Fatal("the ID of a command changes between calls")
	}
	if ID("poll") == ID("me") {
		t.Fatal("two commands share an ID")
	}
}

func TestRegistryListsAvailableCommandsInOrderAndFindsThemByID(t *testing.T) {
	r := NewRegistry()
	for _, c := range []fake{cmd("poll", true), cmd("nick", false), cmd("me", true)} {
		if err := r.Add(c); err != nil {
			t.Fatal(err)
		}
	}
	var names []string
	for _, b := range r.Available(nil, perm.Subject{}, uuid.Nil) {
		names = append(names, b.Definition().Name)
	}
	if len(names) != 2 || names[0] != "poll" || names[1] != "me" {
		t.Fatalf("available: %v", names)
	}
	if _, ok := r.Lookup(ID("nick")); !ok {
		t.Fatal("a registered command is not found")
	}
	if _, ok := r.Lookup(uuid.New()); ok {
		t.Fatal("an unknown ID was found")
	}
}

func TestANilRegistryHasNoCommands(t *testing.T) {
	var r *Registry
	if _, ok := r.Lookup(ID("poll")); ok || len(r.Available(nil, perm.Subject{}, uuid.Nil)) != 0 {
		t.Fatal("a nil registry offers commands")
	}
}

func TestRegistryRefusesMalformedAndDuplicateDefinitions(t *testing.T) {
	r := NewRegistry()
	bad := map[string]fake{
		"upper case":        cmd("Poll", true),
		"empty":             cmd("", true),
		"spaces":            cmd("a b", true),
		"no description":    {def: Definition{Name: "x"}},
		"bad option name":   {def: Definition{Name: "x", Description: "d", Options: []Option{{Name: "A", Description: "d"}}}},
		"option without it": {def: Definition{Name: "x", Description: "d", Options: []Option{{Name: "a"}}}},
	}
	for name, c := range bad {
		if err := r.Add(c); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if err := r.Add(cmd("poll", true)); err != nil {
		t.Fatal(err)
	}
	if err := r.Add(cmd("poll", true)); err == nil {
		t.Fatal("a command was registered twice")
	}
}
