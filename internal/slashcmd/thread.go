package slashcmd

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/slash"
)

// threadCommand is /thread: it opens a thread under the caller's latest
// message in the channel, with the title as the thread's first reply.
type threadCommand struct{ svc *Services }

var _ slash.Builtin = threadCommand{}

// Definition describes /thread for the menu.
func (threadCommand) Definition() slash.Definition {
	return slash.Definition{
		Name: "thread", Description: "Start a thread on your latest message",
		Options: []slash.Option{{Name: "title", Description: "What the thread is about", Type: slash.OptionString, Required: true}},
	}
}

// Available: threads are replies, so whoever may send may start one.
func (threadCommand) Available(*perm.Snapshot, perm.Subject, uuid.UUID) bool { return true }

// Run posts the title as a reply to the caller's latest message of the
// channel that is not itself a reply. The reply goes through the same
// validation as any thread reply (the parent must be a root message of this
// channel).
func (c threadCommand) Run(ctx context.Context, call slash.Call) (slash.Result, error) {
	var root uuid.UUID
	err := c.svc.DB.QueryRow(ctx, `
		SELECT id FROM messages WHERE channel_id = $1 AND user_id = $2 AND parent_id IS NULL
		ORDER BY created_at DESC, id DESC LIMIT 1`, call.Channel, call.User.ID).Scan(&root)
	if errors.Is(err, pgx.ErrNoRows) {
		return slash.Result{}, httpx.ErrInvalidInput("you have no message in this channel to start a thread on")
	}
	if err != nil {
		return slash.Result{}, err
	}
	return c.svc.post(call, str(call, "title"), &root)
}
