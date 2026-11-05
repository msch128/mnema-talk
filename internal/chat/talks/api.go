package talks

import (
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/chat/channels"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/chat/mentions"
	"github.com/msch128/mnema-talk/internal/chat/messages"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// CallStarter rings the members of a conversation when a Talk is started or
// shared in it. It is what talks needs from the direct message calls (dm.DMCalls
// implements it); talks does not import dm, so the two stay independent. A nil
// *dm.DMCalls is a valid starter that rings nobody.
type CallStarter interface {
	Begin(conversation, talk, caller, message uuid.UUID, name string)
}

// API serves the Create-a-Talk routes: renaming a temporary Talk, inviting to
// it and starting one from a conversation. The chat Handler builds it from its
// own fields, so the server wiring is unchanged.
type API struct {
	DB     *db.Pool
	Events events.Publisher
	// Guard decides who may see and use a channel.
	Guard core.Guard
	// Audit records Talk changes; nil records nothing.
	Audit *audit.Log
	// Online resolves @here in invitations; nil means nobody is online.
	Online mentions.OnlineSource
	// Talks runs the Create-a-Talk lobbies; nil switches the feature off.
	Talks *TalkManager
	// Calls rings the members of a conversation when a Talk is started or
	// shared in it; nil means no ringing (the invitation card is still posted).
	Calls CallStarter
	// TalkLimit caps Talk edits and invitations per member; nil means no cap.
	TalkLimit *httpx.RateLimiter
	// Messages posts the invitation cards.
	Messages *messages.API
	// Channels announces channel changes.
	Channels *channels.API
}
