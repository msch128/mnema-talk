// Package slash is the framework for the server's built-in slash commands.
//
// A built-in command is a Go value that describes itself (name, description,
// options) and runs on the server. The composer lists built-ins together with
// the commands of bots (GET /api/channels/{id}/commands) and runs both through
// the same endpoint (POST /api/channels/{id}/interactions), which checks the
// member's right to send, rate-limits, and validates the options against the
// definition before a built-in ever sees them. A new command is one type that
// implements Builtin plus one Registry.Add in the server wiring; the menu, the
// option fields, validation and the permission checks come with the framework.
package slash

import (
	"context"
	"fmt"
	"net/http"
	"regexp"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/perm"
)

// Option types, with the numbers bot commands use (Discord's).
const (
	OptionString  = 3
	OptionInteger = 4
	OptionBoolean = 5
	OptionUser    = 6
	OptionChannel = 7
)

// Option is one argument of a command. Choices, when set, restrict a string
// option to those values.
type Option struct {
	Name        string
	Description string
	Type        int
	Required    bool
	Choices     []string
}

// Definition is what the menu shows about a command. Name and option names
// are lowercase letters, digits, '-' and '_'; the descriptions are English
// fallbacks, the web app shows its own translations.
type Definition struct {
	Name        string
	Description string
	Options     []Option
}

// Call is one invocation, after the framework checked that the member may
// send messages in the channel and that Options fit the definition.
type Call struct {
	// Request is the member's request; Run derives its context from it.
	Request *http.Request
	Channel uuid.UUID
	User    *auth.User
	// Snap is the permission snapshot the invocation was checked against.
	Snap *perm.Snapshot
	// Options maps option names to values: a string, int64, bool, or a
	// uuid.UUID for user and channel options. Optional options that were not
	// given are absent.
	Options map[string]any
}

// Result is what a command did. The message it posted, if any, is published
// by the command itself.
type Result struct {
	MessageID *uuid.UUID `json:"message_id,omitempty" binding:"optional" format:"uuid"`
}

// Builtin is a command the server runs itself.
type Builtin interface {
	Definition() Definition
	// Available says whether the member may use the command in the channel,
	// on top of the right to send messages there (which the framework
	// already required). Use it for commands that need more, such as a
	// moderation shortcut.
	Available(snap *perm.Snapshot, user perm.Subject, channel uuid.UUID) bool
	// Run executes the command. Errors are httpx errors for the member or
	// internal failures.
	Run(ctx context.Context, call Call) (Result, error)
}

var nameRE = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)

// namespace derives the stable IDs of built-in commands.
var namespace = uuid.MustParse("6f0d9a52-6c58-4f0e-9a3b-6b0e3c2a41d7")

// ID is the stable ID a built-in command has in the command list: the same on
// every server, so a client can tell it from a bot's command.
func ID(name string) uuid.UUID {
	return uuid.NewSHA1(namespace, []byte("builtin:"+name))
}

// Registry holds the built-in commands in the order they were added.
type Registry struct {
	list []Builtin
	byID map[uuid.UUID]Builtin
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{byID: map[uuid.UUID]Builtin{}}
}

// Add registers a command. A malformed or duplicate definition is a
// programming error and returns one.
func (r *Registry) Add(b Builtin) error {
	d := b.Definition()
	if !nameRE.MatchString(d.Name) || d.Description == "" {
		return fmt.Errorf("slash: invalid built-in command %q", d.Name)
	}
	id := ID(d.Name)
	if _, dup := r.byID[id]; dup {
		return fmt.Errorf("slash: built-in command %q registered twice", d.Name)
	}
	if err := validOptions(d); err != nil {
		return err
	}
	r.list = append(r.list, b)
	r.byID[id] = b
	return nil
}

// validOptions checks the options of a definition: valid names, no name twice,
// a known type, and choices only on string options.
func validOptions(d Definition) error {
	seen := map[string]bool{}
	for _, o := range d.Options {
		if !nameRE.MatchString(o.Name) || o.Description == "" {
			return fmt.Errorf("slash: command %q has an invalid option %q", d.Name, o.Name)
		}
		if seen[o.Name] {
			return fmt.Errorf("slash: command %q has the option %q twice", d.Name, o.Name)
		}
		seen[o.Name] = true
		switch o.Type {
		case OptionString, OptionInteger, OptionBoolean, OptionUser, OptionChannel:
		default:
			return fmt.Errorf("slash: command %q option %q has the unknown type %d", d.Name, o.Name, o.Type)
		}
		if len(o.Choices) > 0 && o.Type != OptionString {
			return fmt.Errorf("slash: command %q option %q has choices but is not a string option", d.Name, o.Name)
		}
	}
	return nil
}

// Lookup finds a command by its ID in the command list.
func (r *Registry) Lookup(id uuid.UUID) (Builtin, bool) {
	if r == nil {
		return nil, false
	}
	b, ok := r.byID[id]
	return b, ok
}

// Available lists the commands the member may use in the channel, in the
// order they were added.
func (r *Registry) Available(snap *perm.Snapshot, user perm.Subject, channel uuid.UUID) []Builtin {
	if r == nil {
		return nil
	}
	var out []Builtin
	for _, b := range r.list {
		if b.Available(snap, user, channel) {
			out = append(out, b)
		}
	}
	return out
}
