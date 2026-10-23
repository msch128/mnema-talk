package slashcmd

import (
	"context"
	"regexp"
	"strconv"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/slash"
)

// The moderation shortcuts run the same service functions as the admin REST
// routes (auth.Handler.TimeoutMember, BanMember, KickMember), which enforce the
// role hierarchy, the Owner's protection and the limits. What the routes get
// from their middleware is repeated here: the global permission is checked by
// RequirePermission before anything else, and the audit entry that the route
// table writes for a route is written by hand.

var durationRE = regexp.MustCompile(`^(\d{1,7})\s*([smhdw])$`)

var unitSeconds = map[string]int{"s": 1, "m": 60, "h": 3600, "d": 86400, "w": 7 * 86400}

const durationHelp = "duration must look like 30m, 2h, 7d or 2w"

// parseDuration reads "90s", "30m", "2h", "7d" or "2w" as seconds. The bounds
// of a timeout or a ban are checked by the service.
func parseDuration(s string) (int, error) {
	m := durationRE.FindStringSubmatch(s)
	if m == nil {
		return 0, httpx.ErrInvalidInput(durationHelp)
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, httpx.ErrInvalidInput(durationHelp)
	}
	return n * unitSeconds[m[2]], nil
}

// targetName is the username to put in the audit entry; "" when unknown.
func (s *Services) targetName(ctx context.Context, id uuid.UUID) string {
	var name string
	_ = s.DB.QueryRow(ctx, `SELECT username FROM users WHERE id = $1`, id).Scan(&name)
	return name
}

func (s *Services) recordModeration(ctx context.Context, action string, target uuid.UUID, details map[string]any) {
	s.record(ctx, audit.Entry{Action: action, TargetType: "user", TargetID: target.String(), TargetName: s.targetName(ctx, target), Details: details})
}

var reasonOption = slash.Option{Name: "reason", Description: "Shown to the member (200 characters at most)", Type: slash.OptionString}

// timeoutCommand is /timeout @member duration [reason].
type timeoutCommand struct{ svc *Services }

var _ slash.Builtin = timeoutCommand{}

// Definition describes /timeout for the menu.
func (timeoutCommand) Definition() slash.Definition {
	return slash.Definition{
		Name: "timeout", Description: "Send a member into a timeout",
		Options: []slash.Option{
			{Name: "member", Description: "Who gets the timeout", Type: slash.OptionUser, Required: true},
			{Name: "duration", Description: "How long, for example 10m, 2h or 3d", Type: slash.OptionString, Required: true},
			reasonOption,
		},
	}
}

// Available: members who hold Timeout members.
func (timeoutCommand) Available(snap *perm.Snapshot, user perm.Subject, _ uuid.UUID) bool {
	return needs(snap, user, perm.TimeoutMembers)
}

// Run checks the permission again on the server, then runs the route's service.
func (c timeoutCommand) Run(ctx context.Context, call slash.Call) (slash.Result, error) {
	if err := c.svc.Auth.RequirePermission(call.Request, perm.TimeoutMembers); err != nil {
		return slash.Result{}, err
	}
	seconds, err := parseDuration(str(call, "duration"))
	if err != nil {
		return slash.Result{}, err
	}
	target := userOption(call, "member")
	state, err := c.svc.Auth.TimeoutMember(call.Request, target, auth.TimeoutRequest{DurationSeconds: seconds, Reason: str(call, "reason")})
	if err != nil {
		return slash.Result{}, err
	}
	c.svc.recordModeration(ctx, "moderation.timeout", target, map[string]any{"duration_seconds": seconds, "reason": state.Reason})
	return slash.Result{}, nil
}

// banCommand is /ban @member [duration] [reason].
type banCommand struct{ svc *Services }

var _ slash.Builtin = banCommand{}

// Definition describes /ban for the menu.
func (banCommand) Definition() slash.Definition {
	return slash.Definition{
		Name: "ban", Description: "Ban a member",
		Options: []slash.Option{
			{Name: "member", Description: "Who gets banned", Type: slash.OptionUser, Required: true},
			{Name: "duration", Description: "For how long, for example 7d; empty bans for good", Type: slash.OptionString},
			reasonOption,
		},
	}
}

// Available: members who hold Ban members.
func (banCommand) Available(snap *perm.Snapshot, user perm.Subject, _ uuid.UUID) bool {
	return needs(snap, user, perm.BanMembers)
}

// Run checks the permission again on the server, then runs the route's service.
func (c banCommand) Run(ctx context.Context, call slash.Call) (slash.Result, error) {
	if err := c.svc.Auth.RequirePermission(call.Request, perm.BanMembers); err != nil {
		return slash.Result{}, err
	}
	req := auth.BanRequest{Reason: str(call, "reason")}
	if d := str(call, "duration"); d != "" {
		seconds, err := parseDuration(d)
		if err != nil {
			return slash.Result{}, err
		}
		req.DurationSeconds = &seconds
	}
	target := userOption(call, "member")
	if err := c.svc.Auth.BanMember(call.Request, target, req); err != nil {
		return slash.Result{}, err
	}
	details := map[string]any{"reason": req.Reason}
	if req.DurationSeconds != nil {
		details["duration_seconds"] = *req.DurationSeconds
	}
	c.svc.recordModeration(ctx, "moderation.ban", target, details)
	return slash.Result{}, nil
}

// kickCommand is /kick @member [reason].
type kickCommand struct{ svc *Services }

var _ slash.Builtin = kickCommand{}

// Definition describes /kick for the menu.
func (kickCommand) Definition() slash.Definition {
	return slash.Definition{
		Name: "kick", Description: "Sign a member out everywhere and drop them from their Talk",
		Options: []slash.Option{
			{Name: "member", Description: "Who is kicked", Type: slash.OptionUser, Required: true},
			reasonOption,
		},
	}
}

// Available: members who hold Kick members (by default the Owner and
// Administrators only).
func (kickCommand) Available(snap *perm.Snapshot, user perm.Subject, _ uuid.UUID) bool {
	return needs(snap, user, perm.KickMembers)
}

// Run checks the permission again on the server, then kicks. A kick leaves no
// sanction: the member can sign in again at once.
func (c kickCommand) Run(ctx context.Context, call slash.Call) (slash.Result, error) {
	if err := c.svc.Auth.RequirePermission(call.Request, perm.KickMembers); err != nil {
		return slash.Result{}, err
	}
	target := userOption(call, "member")
	if err := c.svc.Auth.KickMember(call.Request, target, str(call, "reason")); err != nil {
		return slash.Result{}, err
	}
	c.svc.recordModeration(ctx, "moderation.kick", target, map[string]any{"reason": str(call, "reason")})
	return slash.Result{}, nil
}
