// Package slashcmd holds the server's built-in slash commands beyond /poll:
// the text commands (/me, /shrug, /tableflip, /unflip, /spoiler), /nick,
// /thread and the moderation shortcuts (/timeout, /ban, /kick).
//
// A command here never decides on its own what a member may do. The slash
// framework (internal/slash, internal/bot) has already checked that the member
// may send in the channel; a command that needs more checks it again on the
// server through the same services as the matching REST route, so a crafted
// request gets exactly the refusal the route would give.
package slashcmd

import (
	"context"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/auth/memberadmin"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/chat/messages"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/perm"
	"github.com/msch128/mnema-talk/internal/slash"
)

// Services is what the commands run on.
type Services struct {
	DB     *db.Pool
	Perms  *perm.Service
	Events events.Publisher
	// Posts posts the text commands' messages (chat.Handler.Posts).
	Posts *messages.Service
	// Members carries the moderation actions shared with the REST
	// routes.
	Members *memberadmin.API
	// Audit records the moderation shortcuts; nil records nothing.
	Audit *audit.Log

	// Chat starts direct messages for /msg (chat.Handler.DMs().StartDM).
	Chat *chat.Handler

	nick *nickLimiter
}

// Commands returns the commands in menu order. It panics without Perms.
func Commands(s Services) []slash.Builtin {
	perm.Must(s.Perms, "built-in slash commands")
	s.nick = newNickLimiter()
	svc := &s
	list := []slash.Builtin{
		textCommand{svc: svc, spec: meSpec},
		textCommand{svc: svc, spec: shrugSpec},
		textCommand{svc: svc, spec: tableflipSpec},
		textCommand{svc: svc, spec: unflipSpec},
		textCommand{svc: svc, spec: spoilerSpec},
		msgCommand{svc: svc},
		nickCommand{svc: svc},
		threadCommand{svc: svc},
		timeoutCommand(svc),
		banCommand(svc),
		kickCommand(svc),
	}
	return list
}

// post stores and announces a message of the caller in the channel, like one
// written in the composer: the caller has to send there and the mentions in
// the text follow their permissions.
func (s *Services) post(call slash.Call, content string, parent *uuid.UUID) (slash.Result, error) {
	posted, err := s.Posts.Post(call.Request.Context(), messages.MemberActor(call.User), messages.PostCommand{
		ChannelID: call.Channel, Content: content, ParentID: parent,
	})
	if err != nil {
		return slash.Result{}, err
	}
	return slash.Result{MessageID: &posted.Message.ID}, nil
}

// str reads a string option; an option that was not given is "".
func str(call slash.Call, name string) string {
	s, _ := call.Options[name].(string)
	return s
}

// userOption reads a member option.
func userOption(call slash.Call, name string) uuid.UUID {
	id, _ := call.Options[name].(uuid.UUID)
	return id
}

// needs reports whether the member holds every permission of need server-wide.
func needs(snap *perm.Snapshot, user perm.Subject, need perm.Set) bool {
	return snap != nil && snap.Global(user).Has(need)
}

// record writes an audit entry for a command that has no REST route of its
// own to be logged by. via marks the entry as made by a slash command.
func (s *Services) record(ctx context.Context, e audit.Entry) {
	if e.Details == nil {
		e.Details = map[string]any{}
	}
	e.Details["via"] = "slash"
	s.Audit.RecordOrLog(ctx, e)
}
