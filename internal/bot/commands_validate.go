package bot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// checkOptions validates the values against the command's options.
func (c *Commands) checkOptions(r *http.Request, snap *perm.Snapshot, user *auth.User, ch uuid.UUID, cmd Command, given map[string]any) ([]map[string]any, error) {
	if err := validateKnownOptions(cmd, given); err != nil {
		return nil, err
	}
	out := make([]map[string]any, 0, len(given))
	for _, o := range cmd.Options {
		entry, err := c.evaluateOption(r, snap, user, ch, o, given)
		if err != nil {
			return nil, err
		}
		if entry != nil {
			out = append(out, entry)
		}
	}
	return out, nil
}

func validateKnownOptions(cmd Command, given map[string]any) error {
	declared := make(map[string]struct{}, len(cmd.Options))
	for _, o := range cmd.Options {
		declared[o.Name] = struct{}{}
	}
	for name := range given {
		if _, ok := declared[name]; !ok {
			return httpx.ErrInvalidInput("unknown option: " + name)
		}
	}
	return nil
}

func (c *Commands) evaluateOption(r *http.Request, snap *perm.Snapshot, user *auth.User, ch uuid.UUID, o Option, given map[string]any) (map[string]any, error) {
	v, present := given[o.Name]
	if !present || v == nil {
		if o.Required {
			return nil, httpx.ErrInvalidInput("missing option: " + o.Name)
		}
		return nil, nil
	}
	val, err := c.checkValue(r, snap, user, ch, o, v)
	if err != nil {
		return nil, err
	}
	return map[string]any{"name": o.Name, "type": o.Type, "value": val}, nil
}

func (c *Commands) checkValue(r *http.Request, snap *perm.Snapshot, user *auth.User, ch uuid.UUID, o Option, v any) (any, error) {
	bad := func(what string) (any, error) {
		return nil, httpx.ErrInvalidInput(fmt.Sprintf("option %s must be %s", o.Name, what))
	}
	switch o.Type {
	case OptionString:
		s, ok := v.(string)
		if !ok {
			return bad("text")
		}
		return checkChoice(o, s)
	case OptionInteger:
		f, ok := v.(float64)
		if !ok || f != math.Trunc(f) || math.Abs(f) > 1<<53 {
			return bad("a whole number")
		}
		return int64(f), nil
	case OptionBoolean:
		b, ok := v.(bool)
		if !ok {
			return bad("true or false")
		}
		return b, nil
	case OptionUser, OptionChannel:
		return c.checkReference(r, snap, user, ch, o.Type, v, bad)
	}
	return bad("valid")
}

// checkChoice cleans a text value and, for an option with choices, requires
// one of them.
func checkChoice(o Option, s string) (any, error) {
	v, err := httpx.CleanText(o.Name, s, MaxOptionValue, o.Required)
	if err != nil || len(o.Choices) == 0 {
		return v, err
	}
	for _, ch := range o.Choices {
		if ch == v {
			return v, nil
		}
	}
	return nil, httpx.ErrInvalidInput(fmt.Sprintf("option %s must be one of its choices", o.Name))
}

// checkReference validates a user or channel option: an ID of a channel the
// invoker can see, or of a member who can see the invocation channel.
func (c *Commands) checkReference(r *http.Request, snap *perm.Snapshot, user *auth.User, ch uuid.UUID, typ int, v any, bad func(string) (any, error)) (any, error) {
	s, ok := v.(string)
	id, err := uuid.Parse(s)
	if !ok || err != nil {
		return bad("an ID")
	}
	if typ == OptionChannel {
		if !snap.Channel(user.Subject(), id).Has(perm.ViewChannel) {
			return bad("a channel you can see")
		}
		return id, nil
	}
	var exists bool
	if err := c.DB.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM users WHERE id = $1 AND NOT is_bot AND `+auth.ActiveSQL("")+`)`, id).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists || !snap.Channel(perm.Subject{ID: id}, ch).Has(perm.ViewChannel) {
		return bad("a member of this channel")
	}
	return id, nil
}

func isValidOptionType(t int) bool {
	switch t {
	case OptionString, OptionInteger, OptionBoolean, OptionUser, OptionChannel:
		return true
	default:
		return false
	}
}

func cleanOptionEntry(o *Option, seen map[string]bool) error {
	if !nameRE.MatchString(o.Name) || seen[o.Name] {
		return httpx.ErrInvalidInput("option names are 1 to 32 lowercase letters, digits, - or _, and unique")
	}
	seen[o.Name] = true
	d, err := httpx.CleanText("option description", o.Description, 100, true)
	if err != nil {
		return err
	}
	o.Description = d
	if !isValidOptionType(o.Type) {
		return httpx.ErrInvalidInput("unknown option type")
	}
	return cleanChoices(o)
}

// MaxChoices bounds the choices of one option.
const MaxChoices = 25

func cleanChoices(o *Option) error {
	if len(o.Choices) == 0 {
		o.Choices = nil
		return nil
	}
	if o.Type != OptionString || len(o.Choices) > MaxChoices {
		return httpx.ErrInvalidInput(fmt.Sprintf("only a text option has choices, at most %d", MaxChoices))
	}
	seen := map[string]bool{}
	for i, ch := range o.Choices {
		v, err := httpx.CleanText("choice", ch, 100, true)
		if err != nil || seen[v] {
			return httpx.ErrInvalidInput("choices are 1 to 100 characters and unique")
		}
		seen[v] = true
		o.Choices[i] = v
	}
	return nil
}

// cleanOptions validates option names, descriptions, types and order, and
// normalizes the descriptions in place.
func cleanOptions(opts []Option) error {
	seen := map[string]bool{}
	optional := false
	for i := range opts {
		if err := cleanOptionEntry(&opts[i], seen); err != nil {
			return err
		}
		// As in Discord, required options come first.
		if opts[i].Required && optional {
			return httpx.ErrInvalidInput("required options must come before optional ones")
		}
		optional = optional || !opts[i].Required
	}
	return nil
}

// CommandPermissionsInput holds default_member_permissions for CommandInput:
// either a decimal bitfield string (e.g. "6") or an array of permission keys
// (e.g. ["ban_members"]). A JSON null or omitting the field clears the
// requirement (available to everyone).
type CommandPermissionsInput struct {
	raw []byte
}

func (c *CommandPermissionsInput) UnmarshalJSON(b []byte) error {
	c.raw = append(c.raw[:0], b...)
	return nil
}

func (c CommandPermissionsInput) MarshalJSON() ([]byte, error) {
	if len(c.raw) == 0 {
		return []byte("null"), nil
	}
	return c.raw, nil
}

// PermissionsString creates a CommandPermissionsInput from a decimal bitfield string.
func PermissionsString(s string) *CommandPermissionsInput {
	b, _ := json.Marshal(s)
	return &CommandPermissionsInput{raw: b}
}

// PermissionsKeys creates a CommandPermissionsInput from permission keys.
func PermissionsKeys(keys ...string) *CommandPermissionsInput {
	if keys == nil {
		keys = []string{}
	}
	b, _ := json.Marshal(keys)
	return &CommandPermissionsInput{raw: b}
}

// Value returns the unquoted string if c represents a string, or nil if null/empty.
func (c *CommandPermissionsInput) Value() *string {
	if c == nil || len(c.raw) == 0 || bytes.Equal(bytes.TrimSpace(c.raw), []byte("null")) {
		return nil
	}
	var s string
	if err := json.Unmarshal(c.raw, &s); err == nil {
		return &s
	}
	str := string(c.raw)
	return &str
}

// cleanDefaultMemberPermissions validates that default_member_permissions is a
// valid decimal string of defined permission bits or an array of permission keys.
// Null/absent returns nil, nil, nil.
func cleanDefaultMemberPermissions(in *CommandPermissionsInput) (*string, *perm.Set, error) {
	if in == nil || len(in.raw) == 0 || bytes.Equal(bytes.TrimSpace(in.raw), []byte("null")) {
		return nil, nil, nil
	}
	var s string
	if err := json.Unmarshal(in.raw, &s); err == nil {
		bits, err := strconv.ParseUint(s, 10, 64)
		if err != nil || !perm.Set(bits).Valid() {
			return nil, nil, httpx.ErrInvalidInput("unknown permission bits")
		}
		canonical := strconv.FormatUint(bits, 10)
		p := perm.Set(bits)
		return &canonical, &p, nil
	}
	var arr []any
	if err := json.Unmarshal(in.raw, &arr); err == nil {
		var set perm.Set
		for _, item := range arr {
			k, ok := item.(string)
			if !ok {
				return nil, nil, httpx.ErrInvalidInput("unknown permission bits")
			}
			info, found := catalogByKey(k)
			if !found {
				return nil, nil, httpx.ErrInvalidInput("unknown permission: " + clipForError(k))
			}
			set |= info.Bit
		}
		canonical := strconv.FormatUint(uint64(set), 10)
		return &canonical, &set, nil
	}
	return nil, nil, httpx.ErrInvalidInput("unknown permission bits")
}

// checkCommandPerm ensures the member holds the command's default permissions
// in the channel, returning 403 naming the missing permission if they do not.
func checkCommandPerm(snap *perm.Snapshot, sub perm.Subject, memberPerms perm.Set, defaultPerms *perm.Set) error {
	if defaultPerms == nil {
		return nil
	}
	if snap.IsOwner(sub) || memberPerms.Has(perm.Administrator) {
		return nil
	}
	if *defaultPerms == 0 {
		return snap.Deny(sub, memberPerms, perm.Administrator)
	}
	return snap.Deny(sub, memberPerms, *defaultPerms)
}

// hasCommandPerm reports whether the member holds the command's default
// permissions in the channel. Owner and Administrators always pass.
func hasCommandPerm(snap *perm.Snapshot, sub perm.Subject, memberPerms perm.Set, defaultPerms *perm.Set) bool {
	return checkCommandPerm(snap, sub, memberPerms, defaultPerms) == nil
}
