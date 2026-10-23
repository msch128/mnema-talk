package poll

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// Mount registers the member routes (RequireUser applies).
func (s *Service) Mount(r chi.Router) {
	r.Post("/channels/{channelID}/polls", httpx.Handle(s.create))
	r.Get("/channels/{channelID}/messages/{messageID}/poll", httpx.Handle(s.get))
	r.Put("/channels/{channelID}/messages/{messageID}/poll/votes", httpx.Handle(s.vote))
	r.Post("/channels/{channelID}/messages/{messageID}/poll/end", httpx.Handle(s.end))
}

// CreateRequest is the body of POST /api/channels/{channelID}/polls.
type CreateRequest struct {
	Question string   `json:"question" minLength:"1" maxLength:"300"`
	Options  []string `json:"options" minItems:"2" maxItems:"10"`
	// Multiple lets members vote for several options.
	Multiple bool `json:"multiple" binding:"optional"`
	// Anonymous hides who voted for what, from everyone.
	Anonymous bool `json:"anonymous" binding:"optional"`
	// Duration is how long voting stays open; 24h when omitted.
	Duration string `json:"duration" binding:"optional" enums:"1h,4h,8h,24h,3d,1w"`
}

// VoteRequest is the body of PUT .../poll/votes.
type VoteRequest struct {
	// OptionIDs replace the caller's votes; an empty list withdraws them.
	OptionIDs []uuid.UUID `json:"option_ids" format:"uuid"`
}

// create handles POST /api/channels/{channelID}/polls.
//
// @Summary Post a poll
// @Description The same as the /poll command: a message whose content is the question, with 2 to 10 options (55 characters each), single or multiple choice, optionally anonymous, open for 1h, 4h, 8h, 24h (default), 3d or 1w. Needs Send messages in the channel. Mentions in the question work as in any message. Broadcasts message_create. A poll cannot be edited.
// @ID createPoll
// @Tags Polls
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param channelID path string true "Channel ID." Format(uuid)
// @Param request body CreateRequest true "Request body."
// @Success 201 {object} chat.Message "The poll message."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): question or options out of bounds, duplicate options, unknown duration."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED)."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing permission."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: no such channel, or it is hidden from the caller."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests, or slow mode."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/channels/{channelID}/polls [post]
func (s *Service) create(w http.ResponseWriter, r *http.Request) error {
	ch, err := httpx.PathUUID(r, "channelID")
	if err != nil {
		return err
	}
	ctx, err := chat.SendContext(s.Perms, r, ch, perm.SendMessages)
	if err != nil {
		return err
	}
	var req CreateRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	msg, err := s.Create(ctx, auth.UserFrom(r.Context()), ch, Input(req))
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusCreated, msg)
	return nil
}

// get handles GET /api/channels/{channelID}/messages/{messageID}/poll.
//
// @Summary Get a poll with my votes
// @Description The poll as in the message, plus the options the caller voted for. This is the only place an anonymous poll shows a member their own choice. Needs View channels.
// @ID getPoll
// @Tags Polls
// @Produce json
// @Security cookieAuth
// @Param channelID path string true "Channel ID." Format(uuid)
// @Param messageID path string true "ID of the poll message." Format(uuid)
// @Success 200 {object} Detail "The poll."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED)."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: no such poll, or the channel is hidden from the caller."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/channels/{channelID}/messages/{messageID}/poll [get]
func (s *Service) get(w http.ResponseWriter, r *http.Request) error {
	rf, err := s.find(r, perm.ViewChannel)
	if err != nil {
		return err
	}
	d, err := s.detail(r.Context(), rf.message, auth.UserFrom(r.Context()).ID)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, d)
	return nil
}

// votePermissions is what voting needs: members who cannot react (a timeout,
// rules not yet accepted) cannot vote either.
const votePermissions = perm.ViewChannel | perm.AddReactions

// vote handles PUT /api/channels/{channelID}/messages/{messageID}/poll/votes.
//
// @Summary Vote in a poll
// @Description Replaces the caller's votes with option_ids (one for a single-choice poll, any number of distinct options for a multiple-choice one; an empty list withdraws the vote). Allowed until the poll ends (409 afterwards). Needs View channels and Add reactions. Broadcasts poll_update with the new tallies; in an anonymous poll nobody learns who voted. At most 60 a minute per member.
// @ID votePoll
// @Tags Polls
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param channelID path string true "Channel ID." Format(uuid)
// @Param messageID path string true "ID of the poll message." Format(uuid)
// @Param request body VoteRequest true "Request body."
// @Success 200 {object} Detail "The poll with the caller's votes."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): unknown or repeated option, several options in a single-choice poll."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED)."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing permission."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: no such poll, or the channel is hidden from the caller."
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT: the poll has ended."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/channels/{channelID}/messages/{messageID}/poll/votes [put]
func (s *Service) vote(w http.ResponseWriter, r *http.Request) error {
	user := auth.UserFrom(r.Context())
	rf, err := s.find(r, votePermissions)
	if err != nil {
		return err
	}
	if ok, retry := s.votes.Allow(user.ID.String()); !ok {
		w.Header().Set("Retry-After", fmt.Sprint(int(retry.Seconds())+1))
		return httpx.ErrRateLimited()
	}
	var req VoteRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	if err := Vote(r.Context(), s.DB, rf.message, user.ID, req.OptionIDs); err != nil {
		return err
	}
	if _, err := s.announce(r.Context(), rf.channel, rf.message); err != nil {
		return err
	}
	d, err := s.detail(r.Context(), rf.message, user.ID)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, d)
	return nil
}

// end handles POST /api/channels/{channelID}/messages/{messageID}/poll/end.
//
// @Summary End a poll early
// @Description The poll's author, or a member with Manage messages in the channel, closes voting now. Ending an ended poll changes nothing. Broadcasts poll_update.
// @ID endPoll
// @Tags Polls
// @Produce json
// @Security cookieAuth
// @Param channelID path string true "Channel ID." Format(uuid)
// @Param messageID path string true "ID of the poll message." Format(uuid)
// @Success 200 {object} Detail "The poll with the caller's votes."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED)."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: neither the author nor allowed to manage messages."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: no such poll, or the channel is hidden from the caller."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/channels/{channelID}/messages/{messageID}/poll/end [post]
func (s *Service) end(w http.ResponseWriter, r *http.Request) error {
	user := auth.UserFrom(r.Context())
	rf, err := s.find(r, perm.ViewChannel)
	if err != nil {
		return err
	}
	if rf.author != user.ID {
		if err := s.Perms.RequireChannel(r.Context(), user.Subject(), rf.channel, perm.ManageMessages); err != nil {
			return err
		}
	}
	changed, err := end(r.Context(), s.DB, rf.message)
	if err != nil {
		return err
	}
	if changed {
		if _, err := s.announce(r.Context(), rf.channel, rf.message); err != nil {
			return err
		}
	}
	d, err := s.detail(r.Context(), rf.message, user.ID)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, d)
	return nil
}
