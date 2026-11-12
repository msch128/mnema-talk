package messages

import (
	"github.com/go-chi/chi/v5"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/chat/mentions"
	"github.com/msch128/mnema-talk/internal/chat/readstate"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// API serves messages: history, posting, editing, deleting, reactions, threads
// and thread follows, pins, forwarding, edit history and bulk deletes. The chat
// Handler builds it from its own fields (so the server wiring is unchanged) and
// shares it with the bot API, which reuses these handlers.
type API struct {
	DB     *db.Pool
	Events events.Publisher
	// Posts posts messages (POST /channels/{channelID}/messages and the bot API).
	Posts *Service
	// Guard decides who may see and use which channel.
	Guard core.Guard
	// Objects deletes the media of removed messages; nil skips it.
	Objects core.ObjectDeleter
	// Online resolves @here; nil means nobody is online.
	Online mentions.OnlineSource
	// Audit records message deletions and edits; nil records nothing.
	Audit *audit.Log
	// Views remembers recent edit-history reads (see edit_history.go); it lives
	// in the Handler so every API built from it shares one memory.
	Views *HistoryViews
	// EditHistoryLimit caps edit-history reads per member; nil means no cap.
	EditHistoryLimit *httpx.RateLimiter
	// PinLimit caps pinning and unpinning per member (each change is broadcast); nil means no cap.
	PinLimit *httpx.RateLimiter
	// FollowLimit caps thread follow changes per member; nil means no cap.
	FollowLimit *httpx.RateLimiter
	// ForwardLimit caps forwarding per member; nil means no cap.
	ForwardLimit *httpx.RateLimiter
}

// Mount registers the message routes (RequireUser must already apply).
func (a *API) Mount(r chi.Router) {
	r.Get("/channels/{channelID}/messages", httpx.Handle(a.ListMessages))
	r.Post("/channels/{channelID}/messages", httpx.Handle(a.ServeCreateMessage))
	r.Put("/channels/{channelID}/messages/{messageID}", httpx.Handle(a.ServeEditMessage))
	r.Delete("/channels/{channelID}/messages/{messageID}", httpx.Handle(a.DeleteMessage))
	a.mountModeration(r)
	a.mountForward(r)
	history := r
	if a.EditHistoryLimit != nil {
		history = r.With(a.EditHistoryLimit.By(auth.UserKey))
	}
	history.Get("/channels/{channelID}/messages/{messageID}/edits", httpx.Handle(a.getEditHistory))
	r.Post("/messages/{messageID}/reactions", httpx.Handle(a.ToggleReaction))
	r.Get("/messages/{messageID}/thread", httpx.Handle(a.GetThread))
	follow := r
	if a.FollowLimit != nil {
		follow = r.With(a.FollowLimit.By(auth.UserKey))
	}
	follow.Put("/threads/{threadID}/follow", httpx.Handle(a.putThreadFollow))
	follow.Delete("/threads/{threadID}/follow", httpx.Handle(a.deleteThreadFollow))
	r.Get("/threads/following", httpx.Handle(a.ListFollowedThreads))
	r.Get("/messages/by-number/{number}", httpx.Handle(a.ServeGetMessageByNumber))
}

// recount tells members their counts of a channel again after a delete or an
// edit there (see readstate.Recount).
func (a *API) recount() readstate.Recount {
	return readstate.Recount{DB: a.DB, Perms: a.Guard.Perms, Online: a.Online, Events: a.Events}
}
