package chat

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/chat/blocks"
	"github.com/msch128/mnema-talk/internal/chat/channels"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/chat/dm"
	"github.com/msch128/mnema-talk/internal/chat/messages"
	"github.com/msch128/mnema-talk/internal/chat/readstate"
	"github.com/msch128/mnema-talk/internal/chat/reports"
	"github.com/msch128/mnema-talk/internal/chat/search"
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
	Objects ObjectDeleter
	// Online resolves @here; nil means nobody is online.
	Online OnlineSource
	// Voice empties the voice room of a deleted channel. When nil, Online is
	// used if it implements VoiceRooms (the WebSocket hub does).
	Voice VoiceRooms
	// Perms decides who may see and use which channel. nil imposes no
	// channel limits (tests); the router always sets it, and without it the
	// admin routes stay Owner-only.
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
	Talks *TalkManager
	// Calls rings the members of a conversation when a Talk is started or shared
	// in it; nil means no ringing (the invitation card is still posted).
	Calls *DMCalls
	// TalkLimit caps Talk edits and invitations per member; nil means no cap.
	TalkLimit *httpx.RateLimiter
}

func (h *Handler) voiceRooms() VoiceRooms {
	if h.Voice != nil {
		return h.Voice
	}
	v, _ := h.Online.(VoiceRooms)
	return v
}

// deleteObjects removes media objects after their rows are gone; see
// core.DeleteObjects.
func (h *Handler) deleteObjects(ctx context.Context, keys []string) {
	core.DeleteObjects(ctx, h.Objects, keys)
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
	return &reports.API{DB: h.DB, Guard: h.guard(), Audit: h.Audit, ReportLimit: h.ReportLimit}
}

// search builds the search API from the handler's fields.
func (h *Handler) search() *search.API {
	return &search.API{DB: h.DB, Guard: h.guard()}
}

// Mount registers the member-facing routes (RequireUser must already apply).
func (h *Handler) Mount(r chi.Router) {
	r.Get("/members", httpx.Handle(h.listMembers))
	blocks.API{DB: h.DB, Events: h.Events, Limit: h.BlockLimit}.Mount(r)
	h.reports().Mount(r)
	h.mountSaved(r)
	h.DMs().Mount(r)
	h.mountTalks(r)
	h.mountForum(r)
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
// @Description Active accounts, the Owner first, then by display name, with voice_seconds and message_count. Enabled bots are members too (is_bot, added_by) and hold the built-in Bot role; incoming-webhook authors are not listed. Disabled accounts remain available in the admin account list and historical messages. Presence and locale are not filled.
// @ID listMembers
// @Tags Users
// @Produce json
// @Security cookieAuth
// @Success 200 {array} Member "Members."
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
	if h.Perms == nil {
		return nil, nil, nil
	}
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

func enrichMembers(members []Member, roles map[uuid.UUID][]uuid.UUID, timeouts map[uuid.UUID]time.Time, hidden map[uuid.UUID]int64) []Member {
	out := make([]Member, len(members))
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
