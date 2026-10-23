package chat

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// Forwarding stores a reference, never a copy. The forwarded message keeps
// the forwarder's own comment as its content and points at the root of what
// was forwarded (forwarding a forward points at the same root). What a reader
// sees of the original is decided when they ask for it
// (GET /api/messages/{id}/forward), by what that reader may see right now:
// nothing about the original travels in the message itself, so message lists,
// WebSocket events, search, push, the inbox, the export and the payloads of
// bots and webhooks cannot show it to the wrong person.

// ForwardsPerMinute is how many messages one member may forward per minute.
const ForwardsPerMinute = 20

// errForward is the one answer to every refusal of a forward: the original
// is not visible, the target is not writable, a conversation refuses the
// message. It says nothing about which, so a forward cannot be used to probe
// channels, conversations or blocks.
var errForward = httpx.ErrForbidden("Couldn't forward this message.")

// ForwardRequest is the body of POST /api/messages/{messageID}/forward.
type ForwardRequest struct {
	// ChannelID is the channel or direct message conversation to forward to.
	ChannelID uuid.UUID `json:"channel_id" format:"uuid"`
	// Comment is the forwarder's own text, optional.
	Comment string `json:"comment" binding:"optional" maxLength:"4000"`
}

// ForwardQuote is the original of a forwarded message as one reader may see
// it. When Available is false nothing else is set (Deleted tells a vanished
// original from one the reader may not see).
type ForwardQuote struct {
	Available bool `json:"available"`
	Deleted   bool `json:"deleted,omitempty" binding:"optional"`
	// The rest is set only when Available.
	MessageID       *uuid.UUID `json:"message_id,omitempty" binding:"optional" format:"uuid"`
	ChannelID       *uuid.UUID `json:"channel_id,omitempty" binding:"optional" format:"uuid"`
	ChannelName     string     `json:"channel_name,omitempty" binding:"optional"`
	Author          *UserRef   `json:"author,omitempty" binding:"optional"`
	Content         string     `json:"content,omitempty" binding:"optional" maxLength:"4000"`
	AttachmentCount int        `json:"attachment_count,omitempty" binding:"optional"`
	CreatedAt       *time.Time `json:"created_at,omitempty" binding:"optional" format:"date-time"`
	IsEdited        bool       `json:"is_edited,omitempty" binding:"optional"`
}

func (h *Handler) mountForward(r chi.Router) {
	r.Get("/messages/{messageID}/forward", httpx.Handle(h.getForwardQuote))
	post := r
	if h.ForwardLimit != nil {
		post = r.With(h.ForwardLimit.By(auth.UserKey))
	}
	post.Post("/messages/{messageID}/forward", httpx.Handle(h.forwardMessage))
}

// forwardRoot is the message a forward would point at.
type forwardRoot struct {
	id      uuid.UUID
	channel uuid.UUID
}

// resolveForwardRoot returns the root of msgID: the message itself, or what
// it forwards. ok is false when there is nothing to forward (a forward whose
// original is gone).
func resolveForwardRoot(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, msgID uuid.UUID) (root forwardRoot, ok bool, err error) {
	var forwarded bool
	var of *uuid.UUID
	var channel uuid.UUID
	err = q.QueryRow(ctx, `SELECT channel_id, forwarded, forward_of FROM messages WHERE id = $1`, msgID).Scan(&channel, &forwarded, &of)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return forwardRoot{}, false, errMessageNotFound
	case err != nil:
		return forwardRoot{}, false, fmt.Errorf("load message: %w", err)
	case !forwarded:
		return forwardRoot{id: msgID, channel: channel}, true, nil
	case of == nil:
		return forwardRoot{}, false, nil
	}
	err = q.QueryRow(ctx, `SELECT channel_id FROM messages WHERE id = $1`, *of).Scan(&channel)
	if errors.Is(err, pgx.ErrNoRows) {
		return forwardRoot{}, false, nil
	}
	if err != nil {
		return forwardRoot{}, false, fmt.Errorf("load original: %w", err)
	}
	return forwardRoot{id: *of, channel: channel}, true, nil
}

// refusedAsForward turns the refusals a forward can meet (no access, no such
// message, a conversation's rules) into errForward and keeps real failures
// and slow mode as they are.
func refusedAsForward(err error) error {
	var apiErr *httpx.APIError
	if errors.As(err, &apiErr) && (apiErr.Status == http.StatusForbidden || apiErr.Status == http.StatusNotFound) {
		return errForward
	}
	return err
}

// forwardMessage handles POST /api/messages/{messageID}/forward.
//
// @Summary Forward a message
// @Description Posts a new message in the target channel or direct message conversation that points at the original; the original's text is never copied, and every reader sees it only if they may see the original. The caller needs to see the original and to send in the target (and the rules of a conversation apply). Forwarding a forward points at its original. The comment is optional. Every refusal answers the same 403 text. The target's slow mode applies. Broadcasts message_create.
// @ID forwardMessage
// @Tags Messages
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param messageID path string true "Message ID." Format(uuid)
// @Param request body ForwardRequest true "Request body."
// @Success 201 {object} Message "The new message."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: the same text for every refusal (no access to the original, no right to send in the target, a conversation that does not take the message), or cross-origin request rejected."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests, or the target's slow mode."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/messages/{messageID}/forward [post]
func (h *Handler) forwardMessage(w http.ResponseWriter, r *http.Request) error {
	msgID, err := httpx.PathUUID(r, "messageID")
	if err != nil {
		return err
	}
	var req ForwardRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	comment, err := ValidateContent(req.Comment, true)
	if err != nil {
		return err
	}
	user := auth.UserFrom(r.Context())
	root, ok, err := resolveForwardRoot(r.Context(), h.DB, msgID)
	if err != nil {
		return refusedAsForward(err)
	}
	if !ok {
		return errForward
	}
	if err := h.require(r, root.channel, perm.ViewChannel); err != nil {
		return refusedAsForward(err)
	}
	ctx, err := h.sendContext(r, req.ChannelID, perm.SendMessages)
	if err != nil {
		return refusedAsForward(err)
	}
	if _, err := ChannelForMessages(ctx, h.DB, req.ChannelID); err != nil {
		return refusedAsForward(err)
	}
	id, err := InsertMessage(ctx, h.DB, h.Online, NewMessage{
		ChannelID: req.ChannelID, UserID: user.ID, Content: comment, ForwardOf: &root.id,
	}, nil)
	if err != nil {
		return refusedAsForward(err)
	}
	return PublishMessage(w, r, h.DB, h.Events, id)
}

// getForwardQuote handles GET /api/messages/{messageID}/forward.
//
// @Summary Read the original of a forwarded message
// @Description The original of a forwarded message as the caller may see it right now: its text, author, time and channel when the caller can see it, otherwise only available=false. Edits and deletes of the original show through. Needs View channel for the forwarded message itself.
// @ID getForwardQuote
// @Tags Messages
// @Produce json
// @Security cookieAuth
// @Param messageID path string true "ID of the forwarded message." Format(uuid)
// @Success 200 {object} ForwardQuote "The quote."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the message does not exist, is not a forward, or the caller cannot see it."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/messages/{messageID}/forward [get]
func (h *Handler) getForwardQuote(w http.ResponseWriter, r *http.Request) error {
	msgID, err := httpx.PathUUID(r, "messageID")
	if err != nil {
		return err
	}
	var channel uuid.UUID
	var forwarded bool
	var of *uuid.UUID
	err = h.DB.QueryRow(r.Context(), `SELECT channel_id, forwarded, forward_of FROM messages WHERE id = $1`, msgID).Scan(&channel, &forwarded, &of)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !forwarded) {
		return errMessageNotFound
	}
	if err != nil {
		return fmt.Errorf("load message: %w", err)
	}
	if err := h.require(r, channel, perm.ViewChannel); err != nil {
		return notFoundUnlessFailure(err)
	}
	if of == nil {
		httpx.WriteJSON(w, http.StatusOK, ForwardQuote{Deleted: true})
		return nil
	}
	quote, err := h.quoteFor(r, *of)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, quote)
	return nil
}

// quoteFor reads the original for the caller; anything they may not see is
// reported as unavailable.
func (h *Handler) quoteFor(r *http.Request, original uuid.UUID) (ForwardQuote, error) {
	ref, err := loadMessageRef(r.Context(), h.DB, original)
	if errors.Is(err, errMessageNotFound) {
		return ForwardQuote{Deleted: true}, nil
	}
	if err != nil {
		return ForwardQuote{}, err
	}
	if err := h.require(r, ref.ChannelID, perm.ViewChannel); err != nil {
		var apiErr *httpx.APIError
		if errors.As(err, &apiErr) {
			return ForwardQuote{}, nil
		}
		return ForwardQuote{}, err
	}
	return loadQuote(r.Context(), h.DB, original)
}

type quoteQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func loadQuote(ctx context.Context, q quoteQuerier, id uuid.UUID) (ForwardQuote, error) {
	out := ForwardQuote{Available: true, MessageID: &id}
	var avatar *string
	var name string
	var created time.Time
	author := UserRef{}
	var channel uuid.UUID
	err := q.QueryRow(ctx, `
		SELECT m.channel_id, CASE WHEN m.in_dm THEN '' ELSE c.name END, u.id, u.username, COALESCE(m.author_name, u.display_name), u.avatar_s3_key,
		       m.content, m.created_at, m.is_edited, (SELECT COUNT(*) FROM media md WHERE md.message_id = m.id)
		FROM messages m JOIN channels c ON c.id = m.channel_id JOIN users u ON u.id = m.user_id
		WHERE m.id = $1`, id).Scan(&channel, &name, &author.ID, &author.Username, &author.DisplayName, &avatar,
		&out.Content, &created, &out.IsEdited, &out.AttachmentCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return ForwardQuote{Deleted: true}, nil
	}
	if err != nil {
		return ForwardQuote{}, fmt.Errorf("load quote: %w", err)
	}
	author.AvatarURL = auth.AvatarURL(avatar)
	out.ChannelID, out.ChannelName, out.Author, out.CreatedAt = &channel, name, &author, &created
	return out, nil
}
