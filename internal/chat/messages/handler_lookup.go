package messages

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat/core"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// ToggleReaction handles POST /api/messages/{messageID}/reactions.
//
// @Summary Toggle a reaction
// @Description Adds the caller's reaction, or removes it if already present. A user may add at most 20 different emoji to one message, and a message carries at most 50 different emoji (409 CONFLICT beyond that). Broadcasts message_reaction.
// @ID toggleReaction
// @Tags Messages
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param messageID path string true "Message ID." Format(uuid)
// @Param request body messages.ReactionRequest true "Request body."
// @Success 200 {object} messages.ReactionResult "Resulting reactions."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Add reactions permission in the channel, a block between the two members of a one-to-one direct message (a new reaction is refused; taking one back still works), or cross-origin request rejected. RULES_NOT_ACCEPTED: the member has not accepted the community rules yet."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT: too many reactions on this message."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/messages/{messageID}/reactions [post]
func (a *API) ToggleReaction(w http.ResponseWriter, r *http.Request) error {
	user := auth.UserFrom(r.Context())
	msgID, err := httpx.PathUUID(r, "messageID")
	if err != nil {
		return err
	}
	var req ReactionRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	emoji, err := core.ValidateReactionRef(req.Emoji)
	if err != nil {
		return err
	}
	ref, err := core.LoadMessageRef(r.Context(), a.DB, msgID)
	if err != nil {
		return err
	}
	p, err := a.Guard.ChannelPerms(r, ref.ChannelID)
	if err != nil {
		return err
	}
	if !p.Has(perm.ViewChannel) {
		return core.ErrMessageNotFound
	}
	// Taking back an own reaction stays possible without the permission.
	reactions, err := ToggleReactionAs(r.Context(), a.DB, msgID, user.ID, emoji, p.Has(perm.AddReactions))
	if err != nil {
		return a.Guard.RefusalFor(r, err, perm.AddReactions)
	}
	payload := ReactionResult{MessageID: msgID, ChannelID: ref.ChannelID, Reactions: reactions}
	a.Events.BroadcastChannel(ref.ChannelID, payload)
	httpx.WriteJSON(w, http.StatusOK, payload)
	return nil
}

func (a *API) loadThreadRoot(r *http.Request, msgID uuid.UUID) (*core.Message, error) {
	root, err := GetMessage(r.Context(), a.DB, msgID)
	if err != nil {
		return nil, err
	}
	if err := a.Guard.Require(r, root.ChannelID, perm.ViewChannel); err != nil {
		return nil, core.NotFoundUnlessFailure(err)
	}
	// Only a root message has a thread; a reply is not one.
	if root.ParentID != nil {
		return nil, core.ErrMessageNotFound
	}
	return root, nil
}

func (a *API) parseThreadQuery(r *http.Request, msgID uuid.UUID) (core.HistoryQuery, error) {
	return a.parseHistoryQuery(r, []string{"before", "after"}, func(ref *core.MessageRef) bool {
		return ref.ParentID != nil && *ref.ParentID == msgID
	})
}

// GetThread handles GET /api/messages/{messageID}/thread.
//
// @Summary Get a thread
// @Description The root message plus a page of replies, oldest first. Use at most one of before, after (reply IDs). The message must be a root message; a reply has no thread (404).
// @ID getThread
// @Tags Messages
// @Produce json
// @Security cookieAuth
// @Param messageID path string true "Message ID." Format(uuid)
// @Param limit query int false "Page size. Values below 1 or non-numeric fall back to the default; larger values are clamped to 100." minimum(1) maximum(100) default(50)
// @Param before query string false "Replies older than this reply." Format(uuid)
// @Param after query string false "Replies newer than this reply." Format(uuid)
// @Success 200 {object} messages.Thread "Thread."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/messages/{messageID}/thread [get]
func (a *API) GetThread(w http.ResponseWriter, r *http.Request) error {
	msgID, err := httpx.PathUUID(r, "messageID")
	if err != nil {
		return err
	}
	root, err := a.loadThreadRoot(r, msgID)
	if err != nil {
		return err
	}
	q, err := a.parseThreadQuery(r, msgID)
	if err != nil {
		return err
	}
	replies, err := GetThreadReplies(r.Context(), a.DB, msgID, q)
	if err != nil {
		return err
	}
	following, err := IsFollowingThread(r.Context(), a.DB, auth.UserFrom(r.Context()).ID, msgID)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, Thread{Root: root, Replies: replies, Following: following})
	return nil
}

// ServeGetMessageByNumber handles GET /api/messages/by-number/{number}.
//
// @Summary Get a message by its link number
// @Description Resolves the short number used in links (/m/4821, /t/4821) to the message, including its channel_id and, for a thread reply, its parent_id.
// @ID getMessageByNumber
// @Tags Messages
// @Produce json
// @Security cookieAuth
// @Param number path int true "Message number." minimum(1)
// @Success 200 {object} core.Message "Message."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/messages/by-number/{number} [get]
func (a *API) ServeGetMessageByNumber(w http.ResponseWriter, r *http.Request) error {
	n, err := strconv.ParseInt(chi.URLParam(r, "number"), 10, 64)
	if err != nil || n < 1 {
		return httpx.ErrInvalidInput("invalid number")
	}
	msg, err := GetMessageByNumber(r.Context(), a.DB, n)
	if err != nil {
		return err
	}
	if err := a.Guard.Require(r, msg.ChannelID, perm.ViewChannel); err != nil {
		return core.NotFoundUnlessFailure(err)
	}
	httpx.WriteJSON(w, http.StatusOK, msg)
	return nil
}

// ReactionRequest is the body of POST /api/messages/{messageID}/reactions.
type ReactionRequest struct {
	// Emoji has no whitespace, control characters, '<' or '>'.
	Emoji string `json:"emoji" maxLength:"32"`
}

// ReactionResult is the reaction summary after a toggle.
type ReactionResult struct {
	MessageID uuid.UUID `json:"message_id" format:"uuid"`
	// ChannelID is the channel of the message.
	ChannelID uuid.UUID              `json:"channel_id" format:"uuid"`
	Reactions []core.ReactionSummary `json:"reactions"`
}

func (ReactionResult) EventName() string { return events.MessageReaction }

// Thread is a root message with a page of its replies.
type Thread struct {
	Root    *core.Message  `json:"root"`
	Replies []core.Message `json:"replies"`
	// Following: replies of this thread notify the caller. Servers before 1.1 do not send it.
	Following bool `json:"following" binding:"optional"`
}
