package slashcmd

import (
	"context"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/slash"
)

// msgCommand is /msg @member text: it opens (or reuses) the direct message
// with the member and writes the text there.
type msgCommand struct{ svc *Services }

var _ slash.Builtin = msgCommand{}

// Definition describes /msg for the menu.
func (msgCommand) Definition() slash.Definition {
	return slash.Definition{
		Name: "msg", Description: "Send a direct message",
		Options: []slash.Option{
			{Name: "member", Description: "Who gets the message", Type: slash.OptionUser, Required: true},
			{Name: "text", Description: "Your message", Type: slash.OptionString, Required: true},
		},
	}
}

// Available: members who may start direct messages. The server re-checks in
// Run through the service behind POST /api/dms.
func (msgCommand) Available(snap *perm.Snapshot, user perm.Subject, _ uuid.UUID) bool {
	return snap != nil && snap.CanStartDM(user)
}

// Run starts the conversation through chat.Handler.StartDM, the service of the
// REST route: Send direct messages, the community switch, blocks, the other
// member's dm_policy and the limits apply, and every refusal about the other
// member is the route's one text. The text is then sent under the
// conversation's id with the permissions of a message in that conversation.
func (c msgCommand) Run(_ context.Context, call slash.Call) (slash.Result, error) {
	h := c.svc.Chat
	if err := h.AllowDMStart(call.Request); err != nil {
		return slash.Result{}, err
	}
	dm, _, err := h.StartDM(call.Request, []uuid.UUID{userOption(call, "member")})
	if err != nil {
		return slash.Result{}, err
	}
	call.Channel = dm.ID
	return c.svc.post(call, str(call, "text"), nil)
}
