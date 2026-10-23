package bot

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/slash"
)

// Built-in commands (internal/slash) share the bots' endpoints: they are
// listed first, marked builtin, and run through the same invoke handler, so
// the composer has one menu and one set of checks.

// builtinCommand shows a built-in command in the shape the list uses.
func builtinCommand(b slash.Builtin) Command {
	d := b.Definition()
	opts := make([]Option, len(d.Options))
	for i, o := range d.Options {
		opts[i] = Option(o)
	}
	return Command{ID: slash.ID(d.Name), Name: d.Name, Description: d.Description, Options: opts}
}

// builtinsFor lists the built-in commands the member may use in the channel.
func (c *Commands) builtinsFor(snap *perm.Snapshot, user perm.Subject, ch uuid.UUID) []ChannelCommand {
	var out []ChannelCommand
	if !snap.Channel(user, ch).Has(perm.SendMessages) {
		return out
	}
	for _, b := range c.Builtins.Available(snap, user, ch) {
		out = append(out, ChannelCommand{Command: builtinCommand(b), Builtin: true})
	}
	return out
}

// runBuiltin runs a built-in command for the caller (who may send in ch).
func (c *Commands) runBuiltin(w http.ResponseWriter, r *http.Request, b slash.Builtin, snap *perm.Snapshot, user *auth.User, ch uuid.UUID, given map[string]any) error {
	if !b.Available(snap, user.Subject(), ch) {
		return errCommandNotFound
	}
	if err := c.checkInvokeRate(w, user.ID.String()); err != nil {
		return err
	}
	checked, err := c.checkOptions(r, snap, user, ch, builtinCommand(b), given)
	if err != nil {
		return err
	}
	values := make(map[string]any, len(checked))
	for _, e := range checked {
		values[e["name"].(string)] = e["value"]
	}
	res, err := b.Run(r.Context(), slash.Call{Request: r, Channel: ch, User: user, Snap: snap, Options: values})
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, res)
	return nil
}
