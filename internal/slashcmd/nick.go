package slashcmd

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/slash"
)

// nickChanges is how many times one member may rename themselves within
// nickWindow: a new name is broadcast to every client.
const (
	nickChanges = 3
	nickWindow  = 10 * time.Minute
)

type nickLimiter struct{ rl *httpx.RateLimiter }

func newNickLimiter() *nickLimiter {
	return &nickLimiter{rl: httpx.NewRateLimiter(nickChanges, nickWindow).Named("slash_nick")}
}

// nickCommand is /nick: it changes the caller's own display name.
type nickCommand struct{ svc *Services }

var _ slash.Builtin = nickCommand{}

// Definition describes /nick for the menu.
func (nickCommand) Definition() slash.Definition {
	return slash.Definition{
		Name: "nick", Description: "Change your display name",
		Options: []slash.Option{{Name: "name", Description: "Your new display name (24 characters at most)", Type: slash.OptionString, Required: true}},
	}
}

// Available: everyone who may send in the channel.
func (nickCommand) Available(*perm.Snapshot, perm.Subject, uuid.UUID) bool { return true }

// Run renames the caller through auth.UpdateProfile, the function behind
// PUT /api/users/me/profile, so the length limit and the cleaning of the text
// are the profile's. Only the caller's own account is ever touched: the target
// is the signed-in member, not an option.
func (c nickCommand) Run(ctx context.Context, call slash.Call) (slash.Result, error) {
	if ok, _ := c.svc.nick.rl.Allow(call.User.ID.String()); !ok {
		return slash.Result{}, httpx.ErrRateLimited()
	}
	u, err := auth.UpdateProfile(ctx, c.svc.DB, call.User.ID, str(call, "name"), call.User.Bio)
	if err != nil {
		return slash.Result{}, err
	}
	c.svc.Events.Broadcast(events.UserUpdate, u.Public())
	return slash.Result{}, nil
}
