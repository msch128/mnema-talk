package bot

import (
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// componentData checks the values of a component use and builds the data of
// its interaction: custom_id, component_type, values for a select and, for a
// user, role or channel select, resolved with what was picked.
func (c *Commands) componentData(ctx context.Context, snap *perm.Snapshot, user *auth.User, ch, botUser uuid.UUID, comp core.Component, values []string) (map[string]any, error) {
	data := map[string]any{"custom_id": comp.CustomID, "component_type": comp.Type}
	if comp.Type == core.ComponentButton {
		if len(values) > 0 {
			return nil, httpx.ErrInvalidInput("a button takes no values")
		}
		return data, nil
	}
	if err := checkValueCount(comp, values); err != nil {
		return nil, err
	}
	if values == nil {
		values = []string{}
	}
	data["values"] = values
	var resolved map[string]any
	var err error
	switch comp.Type {
	case core.ComponentStringSelect:
		err = checkOptionValues(comp, values)
	case core.ComponentUserSelect:
		resolved, err = c.resolveUsers(ctx, snap, ch, values)
	case core.ComponentRoleSelect:
		resolved, err = resolveRoles(snap, values)
	case core.ComponentChannelSelect:
		resolved, err = c.resolveChannels(ctx, snap, user, botUser, values)
	}
	if err != nil {
		return nil, err
	}
	if resolved != nil {
		data["resolved"] = resolved
	}
	return data, nil
}

// checkValueCount requires between min_values and max_values distinct values.
func checkValueCount(comp core.Component, values []string) error {
	lo, hi := core.SelectBounds(comp)
	if len(values) < lo || len(values) > hi {
		return httpx.ErrInvalidInput(fmt.Sprintf("values: this select takes %d to %d values", lo, hi))
	}
	seen := map[string]bool{}
	for _, v := range values {
		if seen[v] {
			return httpx.ErrInvalidInput("values: each value at most once")
		}
		seen[v] = true
	}
	return nil
}

var errBadValue = httpx.ErrInvalidInput("values: not a choice of this select")

func checkOptionValues(comp core.Component, values []string) error {
	for _, v := range values {
		if !slices.ContainsFunc(comp.Options, func(o core.SelectOption) bool { return o.Value == v }) {
			return errBadValue
		}
	}
	return nil
}

func parseIDs(values []string) ([]uuid.UUID, error) {
	ids := make([]uuid.UUID, len(values))
	for i, v := range values {
		id, err := uuid.Parse(v)
		if err != nil {
			return nil, errBadValue
		}
		ids[i] = id
	}
	return ids, nil
}

// resolveUsers accepts what a member option of a command accepts: active
// members (not bots) who can view the channel.
func (c *Commands) resolveUsers(ctx context.Context, snap *perm.Snapshot, ch uuid.UUID, values []string) (map[string]any, error) {
	ids, err := parseIDs(values)
	if err != nil {
		return nil, err
	}
	rows, err := c.DB.Query(ctx, `SELECT id, username, display_name, avatar_s3_key FROM users
		WHERE id = ANY($1) AND NOT is_bot AND `+auth.ActiveSQL("")+``, ids)
	if err != nil {
		return nil, fmt.Errorf("resolve users: %w", err)
	}
	users := map[string]any{}
	var id uuid.UUID
	var username, display string
	var avatar *string
	_, err = pgx.ForEachRow(rows, []any{&id, &username, &display, &avatar}, func() error {
		if snap.Channel(perm.Subject{ID: id}, ch).Has(perm.ViewChannel) {
			users[id.String()] = map[string]any{"id": id, "username": username, "display_name": display, "avatar_url": auth.AvatarURL(avatar)}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("resolve users: %w", err)
	}
	if len(users) != len(ids) {
		return nil, httpx.ErrInvalidInput("values: members of this channel only")
	}
	return map[string]any{"users": users}, nil
}

// resolveRoles accepts roles that exist, except @everyone and the managed
// bot roles.
func resolveRoles(snap *perm.Snapshot, values []string) (map[string]any, error) {
	ids, err := parseIDs(values)
	if err != nil {
		return nil, err
	}
	roles := map[string]any{}
	for _, id := range ids {
		role, ok := snap.Role(id)
		if !ok || role.Everyone || role.Managed {
			return nil, httpx.ErrInvalidInput("values: roles of the community only")
		}
		roles[id.String()] = map[string]any{"id": role.ID, "name": role.Name, "color": role.Color}
	}
	return map[string]any{"roles": roles}, nil
}

// resolveChannels accepts channels (text, voice and forum, never a
// conversation) that the member and the bot can both view: the bot is not
// told the name of a channel it cannot see.
func (c *Commands) resolveChannels(ctx context.Context, snap *perm.Snapshot, user *auth.User, botUser uuid.UUID, values []string) (map[string]any, error) {
	ids, err := parseIDs(values)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		if !snap.Channel(user.Subject(), id).Has(perm.ViewChannel) || !snap.Channel(perm.Subject{ID: botUser}, id).Has(perm.ViewChannel) {
			return nil, errNoSuchChannel
		}
	}
	rows, err := c.DB.Query(ctx, `SELECT id, name, type FROM channels
		WHERE id = ANY($1) AND type IN ('text', 'voice', 'forum') AND dm_id IS NULL`, ids)
	if err != nil {
		return nil, fmt.Errorf("resolve channels: %w", err)
	}
	channels := map[string]any{}
	var id uuid.UUID
	var name, typ string
	_, err = pgx.ForEachRow(rows, []any{&id, &name, &typ}, func() error {
		channels[id.String()] = map[string]any{"id": id, "name": name, "type": typ}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("resolve channels: %w", err)
	}
	if len(channels) != len(ids) {
		return nil, errNoSuchChannel
	}
	return map[string]any{"channels": channels}, nil
}

var errNoSuchChannel = httpx.ErrInvalidInput("values: channels you and the bot can view only")
