package chat

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

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

// SearchResult is one page of search hits.
type SearchResult struct {
	Messages []Message `json:"messages"`
	HasMore  bool      `json:"has_more"`
	// Roots maps a reply hit's parent_id to its thread root.
	Roots map[string]SearchRoot `json:"roots"`
}
