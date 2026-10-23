package bot

import (
	"fmt"
	"math"
	"net/http"

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
	if err := c.DB.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM users WHERE id = $1 AND NOT is_bot AND disabled_at IS NULL)`, id).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists || !snap.Channel(perm.Subject{ID: id}, ch).Has(perm.ViewChannel) {
		return bad("a member of this channel")
	}
	return id, nil
}
