package chat

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/auth"
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
	// BlockLimit caps blocking and unblocking per member; nil means no cap.
	BlockLimit *httpx.RateLimiter
	// ReportLimit caps message reports per member; nil means no cap.
	ReportLimit *httpx.RateLimiter
	// DMLimit caps starting direct messages and adding members to them per
	// member; nil means no cap.
	DMLimit *httpx.RateLimiter
}

// VoiceRooms acts on live voice rooms (implemented by the WebSocket hub).
type VoiceRooms interface {
	// CloseVoiceChannel disconnects everyone from the channel's voice room.
	CloseVoiceChannel(channelID uuid.UUID)
}

func (h *Handler) voiceRooms() VoiceRooms {
	if h.Voice != nil {
		return h.Voice
	}
	v, _ := h.Online.(VoiceRooms)
	return v
}

// ObjectDeleter removes stored media objects (implemented by media.Store).
type ObjectDeleter interface {
	DeleteBatch(ctx context.Context, keys []string) error
}

// deleteObjects removes media objects after their rows are gone. A failure
// only leaves an orphaned object behind, so it is logged, not returned.
func (h *Handler) deleteObjects(ctx context.Context, keys []string) {
	if h.Objects == nil || len(keys) == 0 {
		return
	}
	if err := h.Objects.DeleteBatch(ctx, keys); err != nil {
		slog.Warn("delete media objects", "count", len(keys), "err", err)
	}
}

// Mount registers the member-facing routes (RequireUser must already apply).
func (h *Handler) Mount(r chi.Router) {
	r.Get("/members", httpx.Handle(h.listMembers))
	r.Get("/channels", httpx.Handle(h.listChannels))
	r.Get("/channels/{channelID}/messages", httpx.Handle(h.listMessages))
	r.Post("/channels/{channelID}/messages", httpx.Handle(h.createMessage))
	r.Put("/channels/{channelID}/messages/{messageID}", httpx.Handle(h.editMessage))
	r.Delete("/channels/{channelID}/messages/{messageID}", httpx.Handle(h.deleteMessage))
	h.mountModeration(r)
	h.mountBlocks(r)
	h.mountReports(r)
	h.mountDMs(r)
	history := r
	if h.EditHistoryLimit != nil {
		history = r.With(h.EditHistoryLimit.By(auth.UserKey))
	}
	history.Get("/channels/{channelID}/messages/{messageID}/edits", httpx.Handle(h.getEditHistory))
	r.Post("/messages/{messageID}/reactions", httpx.Handle(h.toggleReaction))
	r.Get("/messages/{messageID}/thread", httpx.Handle(h.getThread))
	r.Get("/messages/by-number/{number}", httpx.Handle(h.getMessageByNumber))
	r.Get("/read-state", httpx.Handle(h.readState))
	r.Get("/search", httpx.Handle(h.search))
	r.Get("/mentions", httpx.Handle(h.mentionsInbox))
	r.Post("/channels/{channelID}/read", httpx.Handle(h.markRead))
	r.Post("/channels/{channelID}/unread", httpx.Handle(h.markUnread))
	r.Get("/channels/{channelID}/first-unread", httpx.Handle(h.firstUnread))
	r.Put("/channels/{channelID}/notifications", httpx.Handle(h.setNotifyLevel))
	r.Put("/categories/{id}/notifications", httpx.Handle(h.setCategoryNotify))
	r.Get("/notification-settings", httpx.Handle(h.notificationSettings))
	r.Post("/read-state/read-all", httpx.Handle(h.markAllRead))
}

// MountAdmin registers channel and category management (the Manage channels
// permission applies).
func (h *Handler) MountAdmin(r chi.Router) {
	h.mountReportsAdmin(r)
	r = r.With(auth.RequirePerm(h.Perms, perm.ManageChannels))
	r.Post("/categories", httpx.Handle(h.createCategory))
	r.Delete("/categories/{id}", httpx.Handle(h.deleteCategory))
	r.Post("/channels", httpx.Handle(h.createChannel))
	r.Delete("/channels/{id}", httpx.Handle(h.deleteChannel))
	r.Patch("/channels/{id}", httpx.Handle(h.updateChannel))
	r.Post("/channels/{id}/duplicate", httpx.Handle(h.duplicateChannel))
	r.Patch("/categories/{id}", httpx.Handle(h.renameCategory))
	r.Put("/layout", httpx.Handle(h.applyLayout))
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
	members, err := GetAllMembers(r.Context(), h.DB)
	if err != nil {
		return err
	}
	roles, timeouts, err := h.loadMemberPermissions(r.Context())
	if err != nil {
		return err
	}
	counts, err := h.visibleMessageCounts(r)
	if err != nil {
		return err
	}
	out := enrichMembers(members, roles, timeouts, counts)
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

func enrichMembers(members []Member, roles map[uuid.UUID][]uuid.UUID, timeouts map[uuid.UUID]time.Time, counts map[uuid.UUID]int64) []Member {
	out := make([]Member, len(members))
	for i, m := range members {
		m.RoleIDs = roles[m.ID]
		out[i] = m
		if until, ok := timeouts[m.ID]; ok {
			out[i].TimeoutUntil = &until
		}
		if counts != nil {
			// Totals must not reveal activity in channels the viewer cannot see.
			out[i].MessageCount = counts[m.ID]
		}
	}
	return out
}

// listChannels handles GET /api/channels.
//
// @Summary List categories and channels
// @Description Only the channels the caller may view; a category shows when its own permissions allow it or any of its channels is visible.
// @ID listChannels
// @Tags Channels
// @Produce json
// @Security cookieAuth
// @Success 200 {object} ChannelHierarchy "Channel hierarchy."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/channels [get]
func (h *Handler) listChannels(w http.ResponseWriter, r *http.Request) error {
	cats, uncat, err := GetServerHierarchy(r.Context(), h.DB)
	if err != nil {
		return err
	}
	cats, uncat, err = h.visibleHierarchy(r, cats, uncat)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, ChannelHierarchy{Categories: cats, Uncategorized: uncat})
	return nil
}
