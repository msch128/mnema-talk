package chat

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// toggleReaction handles POST /api/messages/{messageID}/reactions.
//
// @Summary Toggle a reaction
// @Description Adds the caller's reaction, or removes it if already present. A user may add at most 20 different emoji to one message, and a message carries at most 50 different emoji (409 CONFLICT beyond that). Broadcasts message_reaction.
// @ID toggleReaction
// @Tags Messages
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param messageID path string true "Message ID." Format(uuid)
// @Param request body ReactionRequest true "Request body."
// @Success 200 {object} ReactionResult "Resulting reactions."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Add reactions permission in the channel, or cross-origin request rejected. RULES_NOT_ACCEPTED: the member has not accepted the community rules yet."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT: too many reactions on this message."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/messages/{messageID}/reactions [post]
func (h *Handler) toggleReaction(w http.ResponseWriter, r *http.Request) error {
	user := auth.UserFrom(r.Context())
	msgID, err := httpx.PathUUID(r, "messageID")
	if err != nil {
		return err
	}
	var req ReactionRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	emoji, err := validateReactionRef(req.Emoji)
	if err != nil {
		return err
	}
	ref, err := loadMessageRef(r.Context(), h.DB, msgID)
	if err != nil {
		return err
	}
	p, err := h.channelPerms(r, ref.ChannelID)
	if err != nil {
		return err
	}
	if !p.Has(perm.ViewChannel) {
		return errMessageNotFound
	}
	// Taking back an own reaction stays possible without the permission.
	reactions, err := ToggleReactionAs(r.Context(), h.DB, msgID, user.ID, emoji, p.Has(perm.AddReactions))
	if err != nil {
		return h.refusalFor(r, err, perm.AddReactions)
	}
	payload := ReactionResult{MessageID: msgID, ChannelID: ref.ChannelID, Reactions: reactions}
	h.Events.BroadcastChannel(ref.ChannelID, events.MessageReaction, payload)
	httpx.WriteJSON(w, http.StatusOK, payload)
	return nil
}

func (h *Handler) loadThreadRoot(r *http.Request, msgID uuid.UUID) (*Message, error) {
	root, err := GetMessage(r.Context(), h.DB, msgID)
	if err != nil {
		return nil, err
	}
	if err := h.require(r, root.ChannelID, perm.ViewChannel); err != nil {
		return nil, notFoundUnlessFailure(err)
	}
	// Only a root message has a thread; a reply is not one.
	if root.ParentID != nil {
		return nil, errMessageNotFound
	}
	return root, nil
}

func (h *Handler) parseThreadQuery(r *http.Request, msgID uuid.UUID) (HistoryQuery, error) {
	return h.parseHistoryQuery(r, []string{"before", "after"}, func(ref *messageRef) bool {
		return ref.ParentID != nil && *ref.ParentID == msgID
	})
}

// getThread handles GET /api/messages/{messageID}/thread.
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
// @Success 200 {object} Thread "Thread."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/messages/{messageID}/thread [get]
func (h *Handler) getThread(w http.ResponseWriter, r *http.Request) error {
	msgID, err := httpx.PathUUID(r, "messageID")
	if err != nil {
		return err
	}
	root, err := h.loadThreadRoot(r, msgID)
	if err != nil {
		return err
	}
	q, err := h.parseThreadQuery(r, msgID)
	if err != nil {
		return err
	}
	replies, err := GetThreadReplies(r.Context(), h.DB, msgID, q)
	if err != nil {
		return err
	}
	following, err := IsFollowingThread(r.Context(), h.DB, auth.UserFrom(r.Context()).ID, msgID)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, Thread{Root: root, Replies: replies, Following: following})
	return nil
}

// getMessageByNumber handles GET /api/messages/by-number/{number}.
//
// @Summary Get a message by its link number
// @Description Resolves the short number used in links (/m/4821, /t/4821) to the message, including its channel_id and, for a thread reply, its parent_id.
// @ID getMessageByNumber
// @Tags Messages
// @Produce json
// @Security cookieAuth
// @Param number path int true "Message number." minimum(1)
// @Success 200 {object} Message "Message."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/messages/by-number/{number} [get]
func (h *Handler) getMessageByNumber(w http.ResponseWriter, r *http.Request) error {
	n, err := strconv.ParseInt(chi.URLParam(r, "number"), 10, 64)
	if err != nil || n < 1 {
		return httpx.ErrInvalidInput("invalid number")
	}
	msg, err := GetMessageByNumber(r.Context(), h.DB, n)
	if err != nil {
		return err
	}
	if err := h.require(r, msg.ChannelID, perm.ViewChannel); err != nil {
		return notFoundUnlessFailure(err)
	}
	httpx.WriteJSON(w, http.StatusOK, msg)
	return nil
}

// search handles GET /api/search.
//
// @Summary Search messages
// @Description Text channels and voice channels' chats (with dm=true, the caller's direct messages instead), newest first (or by sort). Every term (max 8) must occur case-insensitively; a quoted phrase is one term and a leading minus excludes messages containing it. At least one of q, channel_id, author_id, mentions, has, is, after, before_date, during, filename or author is required. Reply hits carry parent_id; roots maps those ids to the thread root with a snippet.
// @ID searchMessages
// @Tags Messages
// @Produce json
// @Security cookieAuth
// @Param q query string false "Search terms, whitespace separated; a quoted phrase is one term, a leading minus excludes a word." maxlength(200)
// @Param channel_id query string false "Only messages of this channel." Format(uuid)
// @Param author_id query string false "Only messages of this author." Format(uuid)
// @Param author query string false "Only messages by this author type: bot, webhook or member." Enums(bot,webhook,member)
// @Param has query string false "Only messages with a file, an image, a video, a link, an embed, a poll or an edit." Enums(file,image,video,link,embed,poll,edit)
// @Param is query string false "Only pinned messages." Enums(pinned)
// @Param mentions query string false "Only messages that mention this user (directly or via @all/@here)." Format(uuid)
// @Param filename query string false "Only messages with an attachment matching this file name (case-insensitive substring)." maxlength(200)
// @Param after query string false "Only messages created after this instant (RFC 3339); must be earlier than before_date." Format(date-time)
// @Param before_date query string false "Only messages created before this instant (RFC 3339)." Format(date-time)
// @Param during query string false "Only messages created during this day (YYYY-MM-DD) in the caller's timezone." Format(date)
// @Param tz query string false "Timezone offset for during (e.g. +02:00, -0500, or minutes east of UTC; a signed run of three digits such as -300 is ambiguous and refused)." default(UTC)
// @Param before query string false "Page back: only messages older (or newer, if sort=oldest) than this message." Format(uuid)
// @Param sort query string false "Sort order: newest (default) or oldest. relevance is accepted as an alias of newest: search has no ranking." Enums(newest,oldest,relevance)
// @Param dm query boolean false "Search the caller's direct message conversations instead of channels. Only conversations the caller is in are searched; channel_id may name one of them." default(false)
// @Param limit query int false "Page size. Values below 1 or non-numeric fall back to the default; larger values are clamped to 50." minimum(1) maximum(50) default(25)
// @Success 200 {object} SearchResult "One page of results."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the before cursor no longer exists (it was deleted since the previous page); reload the list."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/search [get]
func (h *Handler) search(w http.ResponseWriter, r *http.Request) error {
	v := r.URL.Query()
	authorType := v.Get("author")
	if authorType == "" {
		authorType = v.Get("author_type")
	}
	tz := v.Get("tz")
	if tz == "" {
		tz = v.Get("tz_offset")
	}
	q := SearchQuery{
		Text:          v.Get("q"),
		Has:           v.Get("has"),
		Is:            v.Get("is"),
		Filename:      v.Get("filename"),
		AuthorType:    authorType,
		Sort:          v.Get("sort"),
		Limit:         httpx.QueryLimit(r, "limit", 25, 50),
		Conversations: v.Get("dm") == "true",
		Member:        auth.UserFrom(r.Context()).ID,
	}
	if _, err := queryIDs(r, "an id", map[string]**uuid.UUID{"channel_id": &q.ChannelID, "author_id": &q.AuthorID, "before": &q.Before, "mentions": &q.Mentions}); err != nil {
		return err
	}
	var err error
	if q.After, err = ParseSearchTime("after", v.Get("after")); err != nil {
		return err
	}
	if q.BeforeDate, err = ParseSearchTime("before_date", v.Get("before_date")); err != nil {
		return err
	}
	if during := v.Get("during"); during != "" {
		if q.DuringStart, q.DuringEnd, err = ParseSearchDay(during, tz); err != nil {
			return err
		}
	}
	if q.Visible, err = h.viewableChannels(r); err != nil {
		return err
	}
	if q.ChannelID != nil {
		if err := h.require(r, *q.ChannelID, perm.ViewChannel); err != nil {
			return err
		}
	}
	release, ok := searchSlots.acquire(auth.UserFrom(r.Context()).ID)
	if !ok {
		return errSearchBusy
	}
	defer release()
	msgs, more, err := Search(r.Context(), h.DB, q)
	if err != nil {
		return err
	}
	roots, err := searchRoots(r.Context(), h.DB, msgs)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, SearchResult{Messages: msgs, HasMore: more, Roots: roots})
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
	ChannelID uuid.UUID         `json:"channel_id" format:"uuid"`
	Reactions []ReactionSummary `json:"reactions"`
}

// Thread is a root message with a page of its replies.
type Thread struct {
	Root    *Message  `json:"root"`
	Replies []Message `json:"replies"`
	// Following: replies of this thread notify the caller. Servers before 1.1 do not send it.
	Following bool `json:"following" binding:"optional"`
}

// SearchResult is one page of search hits.
type SearchResult struct {
	Messages []Message `json:"messages"`
	HasMore  bool      `json:"has_more"`
	// Roots maps a reply hit's parent_id to its thread root.
	Roots map[string]SearchRoot `json:"roots"`
}
