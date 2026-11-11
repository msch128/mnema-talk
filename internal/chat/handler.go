package chat

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/chat/blocks"
	"github.com/msch128/mnema-talk/internal/chat/botapi"
	"github.com/msch128/mnema-talk/internal/chat/channels"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/chat/dm"
	"github.com/msch128/mnema-talk/internal/chat/forum"
	"github.com/msch128/mnema-talk/internal/chat/mentions"
	"github.com/msch128/mnema-talk/internal/chat/messages"
	"github.com/msch128/mnema-talk/internal/chat/readstate"
	"github.com/msch128/mnema-talk/internal/chat/reports"
	"github.com/msch128/mnema-talk/internal/chat/saved"
	"github.com/msch128/mnema-talk/internal/chat/search"
	"github.com/msch128/mnema-talk/internal/chat/talks"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// Handler serves channels, messages, threads and reactions.
type Handler struct {
	DB     *db.Pool
	Events events.Publisher
	// Objects deletes the media of removed messages and channels; nil skips it.
	Objects core.ObjectDeleter
	// Online resolves @here; nil means nobody is online.
	Online mentions.OnlineSource
	// Voice empties the voice room of a deleted channel. When nil, Online is
	// used if it implements VoiceRooms (the WebSocket hub does).
	Voice core.VoiceRooms
	// Perms decides who may see and use which channel. It is required:
	// mounting a Handler without it panics.
	Perms *perm.Service
	// Audit records message deletions and edits; nil records nothing.
	Audit *audit.Log
	// EditHistoryLimit caps edit-history reads per member; nil means no cap.
	EditHistoryLimit *httpx.RateLimiter
	// PinLimit caps pinning and unpinning per member (each change is broadcast); nil means no cap.
	PinLimit *httpx.RateLimiter
	// FollowLimit caps thread follow changes per member; nil means no cap.
	FollowLimit *httpx.RateLimiter

	historyViews messages.HistoryViews
	// BlockLimit caps blocking and unblocking per member; nil means no cap.
	BlockLimit *httpx.RateLimiter
	// ReportLimit caps message reports per member; nil means no cap.
	ReportLimit *httpx.RateLimiter
	// SavedLimit caps saving and removing saved messages per member; nil means no cap.
	SavedLimit *httpx.RateLimiter
	// ForwardLimit caps forwarding per member; nil means no cap.
	ForwardLimit *httpx.RateLimiter
	// DMLimit caps starting direct messages and adding members to them per
	// member; nil means no cap.
	DMLimit *httpx.RateLimiter
	// Talks runs the Create-a-Talk lobbies; nil switches the feature off.
	Talks *talks.TalkManager
	// Calls rings the members of a conversation when a Talk is started or shared
	// in it; nil means no ringing (the invitation card is still posted).
	Calls *dm.DMCalls
	// TalkLimit caps Talk edits and invitations per member; nil means no cap.
	TalkLimit *httpx.RateLimiter
}

func (h *Handler) voiceRooms() core.VoiceRooms {
	if h.Voice != nil {
		return h.Voice
	}
	v, _ := h.Online.(core.VoiceRooms)
	return v
}

// messages builds the messages API from the handler's fields. The edit-history
// memory is shared, so every API built here counts the same reads.
func (h *Handler) messages() *messages.API {
	return &messages.API{
		DB: h.DB, Events: h.Events, Guard: h.guard(), Objects: h.Objects, Online: h.Online, Audit: h.Audit,
		Views: &h.historyViews, EditHistoryLimit: h.EditHistoryLimit, PinLimit: h.PinLimit,
		FollowLimit: h.FollowLimit, ForwardLimit: h.ForwardLimit,
	}
}

// channels builds the channels API from the handler's fields.
func (h *Handler) channels() *channels.API {
	return &channels.API{DB: h.DB, Events: h.Events, Guard: h.guard(), Objects: h.Objects, Voice: h.voiceRooms()}
}

// DMs is the direct message API of this handler, for the services that start
// conversations on a member's behalf (the /msg command).
func (h *Handler) DMs() *dm.API {
	return &dm.API{DB: h.DB, Events: h.Events, Guard: h.guard(), Objects: h.Objects, Voice: h.voiceRooms(), Calls: h.Calls, DMLimit: h.DMLimit}
}

// reports builds the reports API from the handler's fields.
func (h *Handler) reports() *reports.API {
	return &reports.API{DB: h.DB, Guard: h.guard(), Audit: h.Audit, ReportLimit: h.ReportLimit, Events: h.Events}
}

// search builds the search API from the handler's fields.
func (h *Handler) search() *search.API {
	return &search.API{DB: h.DB, Guard: h.guard()}
}

// forum builds the forum API from the handler's fields.
func (h *Handler) forum() *forum.API {
	return &forum.API{DB: h.DB, Events: h.Events, Guard: h.guard(), Audit: h.Audit, Online: h.Online, Messages: h.messages()}
}

// saved builds the saved messages API from the handler's fields.
func (h *Handler) saved() *saved.API {
	return &saved.API{DB: h.DB, Events: h.Events, Guard: h.guard(), SavedLimit: h.SavedLimit}
}

// talks builds the Create-a-Talk API from the handler's fields. h.Calls may be
// nil: a nil *dm.DMCalls rings nobody.
func (h *Handler) talks() *talks.API {
	return &talks.API{
		DB: h.DB, Events: h.Events, Guard: h.guard(), Audit: h.Audit, Online: h.Online, Talks: h.Talks,
		Calls: h.Calls, TalkLimit: h.TalkLimit, Messages: h.messages(), Channels: h.channels(),
	}
}

// MountBot registers the bot API routes (the bot authentication middleware must
// already apply); see botapi.API.MountBot.
func (h *Handler) MountBot(r chi.Router, send func(http.Handler) http.Handler) {
	(&botapi.API{DB: h.DB, Events: h.Events, Guard: h.guard(), Online: h.Online, Messages: h.messages(), Channels: h.channels()}).MountBot(r, send)
}

// Mount registers the member-facing routes (RequireUser must already apply).
func (h *Handler) Mount(r chi.Router) {
	r.Get("/members", httpx.Handle(h.listMembers))
	blocks.API{DB: h.DB, Events: h.Events, Limit: h.BlockLimit}.Mount(r)
	h.reports().Mount(r)
	h.saved().Mount(r)
	h.DMs().Mount(r)
	h.talks().Mount(r)
	h.forum().Mount(r)
	h.messages().Mount(r)
	h.search().Mount(r)
	h.channels().Mount(r)
	readstate.API{DB: h.DB, Events: h.Events, Guard: h.guard()}.Mount(r)
}

// MountAdmin registers channel and category management (the Manage channels
// permission applies).
func (h *Handler) MountAdmin(r chi.Router) {
	h.reports().MountAdmin(r)
	h.channels().MountAdmin(r)
}

// listMembers handles GET /api/members.
//
// @Summary List members
// @Description Active accounts, the Owner first, then by display name, with voice_seconds (time in Talks @everyone can see) and message_count. Enabled bots are members too (is_bot, added_by) and hold the built-in Bot role; incoming-webhook authors are not listed. Disabled accounts remain available in the admin account list and historical messages. Presence and locale are not filled.
// @ID listMembers
// @Tags Users
// @Produce json
// @Security cookieAuth
// @Success 200 {array} core.Member "Members."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/members [get]
func (h *Handler) listMembers(w http.ResponseWriter, r *http.Request) error {
	members, err := core.GetAllMembers(r.Context(), h.DB)
	if err != nil {
		return err
	}
	roles, timeouts, err := h.loadMemberPermissions(r.Context())
	if err != nil {
		return err
	}
	hidden, err := h.hiddenMessageCounts(r)
	if err != nil {
		return err
	}
	out := enrichMembers(members, roles, timeouts, hidden)
	httpx.WriteJSON(w, http.StatusOK, out)
	return nil
}

func (h *Handler) loadMemberPermissions(ctx context.Context) (map[uuid.UUID][]uuid.UUID, map[uuid.UUID]time.Time, error) {
	roles, err := h.Perms.MemberRoles(ctx)
	if err != nil {
		return nil, nil, err
	}
	snap, err := h.Perms.Snapshot(ctx)
	if err != nil {
		return nil, nil, err
	}
	return roles, snap.Timeouts(), nil
}

func enrichMembers(members []core.Member, roles map[uuid.UUID][]uuid.UUID, timeouts map[uuid.UUID]time.Time, hidden map[uuid.UUID]int64) []core.Member {
	out := make([]core.Member, len(members))
	for i, m := range members {
		m.RoleIDs = roles[m.ID]
		out[i] = m
		if until, ok := timeouts[m.ID]; ok {
			out[i].TimeoutUntil = &until
		}
		if hidden != nil {
			// Totals must not reveal activity in channels the viewer cannot see.
			out[i].MessageCount = max(m.MessageCount-hidden[m.ID], 0)
		}
	}
	return out
}
