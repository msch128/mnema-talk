// Package emoji serves the custom emoji of the instance: members list and
// use them, holders of Manage emoji upload, rename and delete them.
//
// An upload is never stored as sent: Process decodes it, redraws it at most
// 128x128 and writes a new PNG or GIF, so metadata and hidden payloads
// (polyglots) cannot survive. The images are served from the same origin with
// their stored content type and a cache lifetime of a year (the URL carries
// the content hash).
package emoji

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/emojitext"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/media"
	"github.com/msch128/mnema-talk/internal/perm"
)

// DefaultMax is how many custom emoji an instance holds unless EMOJI_MAX says
// otherwise.
const DefaultMax = 200

// multipartMemory keeps the form in memory: the whole body is capped at 1 MiB.
const multipartMemory = 2 << 20

// Handler serves /api/emoji and /api/admin/emoji.
type Handler struct {
	DB     *db.Pool
	Store  media.Store
	Events events.Publisher
	Perms  *perm.Service
	// Max is the most custom emoji the instance holds (EMOJI_MAX); 0 forbids
	// new ones.
	Max int
}

// Mount registers the routes every signed-in member may call.
func (h *Handler) Mount(r chi.Router) {
	r.Get("/emoji", httpx.Handle(h.listEmoji))
	r.Get("/emoji/{id}/image", httpx.Handle(h.image))
	r.Get("/emoji/{id}/still", httpx.Handle(h.still))
}

// MountAdmin registers the management routes (Manage emoji).
func (h *Handler) MountAdmin(r chi.Router) {
	r = r.With(auth.RequirePerm(h.Perms, perm.ManageEmoji))
	r.Post("/emoji", httpx.Handle(h.create))
	r.Patch("/emoji/{id}", httpx.Handle(h.update))
	r.Delete("/emoji/{id}", httpx.Handle(h.delete))
}

func (h *Handler) changed() { h.Events.Broadcast(events.EmojiChanged, nil) }

// listEmoji handles GET /api/emoji.
//
// @Summary List custom emoji
// @Description Every custom emoji of the instance, by name, with the instance limit. Members use them as `<:name:id>` in messages and reactions.
// @ID listEmoji
// @Tags Emoji
// @Produce json
// @Security cookieAuth
// @Success 200 {object} List "Custom emoji."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/emoji [get]
func (h *Handler) listEmoji(w http.ResponseWriter, r *http.Request) error {
	items, err := h.list(r.Context())
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, List{Emoji: items, Limit: h.Max})
	return nil
}

// readUpload reads the name and the validated, redrawn image of a request.
func readUpload(r *http.Request) (string, *Image, error) {
	if err := r.ParseMultipartForm(multipartMemory); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return "", nil, httpx.ErrPayloadTooLarge(fmt.Sprintf("the emoji file exceeds %d KiB", MaxFileBytes>>10))
		}
		return "", nil, httpx.ErrInvalidInput("invalid multipart form")
	}
	name, err := cleanName(r.FormValue("name"))
	if err != nil {
		return "", nil, err
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		return "", nil, httpx.ErrInvalidInput("file is required")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, MaxFileBytes+1))
	if err != nil {
		return "", nil, httpx.ErrInvalidInput("the file cannot be read")
	}
	if len(data) > MaxFileBytes {
		return "", nil, httpx.ErrPayloadTooLarge(fmt.Sprintf("the emoji file exceeds %d KiB", MaxFileBytes>>10))
	}
	if err := checkType(header.Header.Get("Content-Type"), header.Filename, data); err != nil {
		return "", nil, err
	}
	img, err := Process(data)
	if err != nil {
		return "", nil, httpx.ErrInvalidInput(strings.TrimPrefix(err.Error(), ErrImage.Error()+": "))
	}
	return name, img, nil
}

// checkType applies the media pipeline's content sniffing, then narrows it to
// the types an emoji may have.
func checkType(declared, filename string, data []byte) error {
	sniffed, err := media.SniffImage(declared, filename, data[:min(len(data), 512)])
	if err != nil || sniffed == "image/jpeg" {
		return httpx.ErrUnsupportedMediaType("an emoji must be a PNG, GIF or WebP image")
	}
	return nil
}

func cleanName(raw string) (string, error) {
	name := strings.ToLower(strings.TrimSpace(raw))
	if !emojitext.ValidName(name) {
		return "", httpx.ErrInvalidInput("name must be 2 to 32 characters: a-z, 0-9 and _")
	}
	return name, nil
}

func (h *Handler) putObjects(ctx context.Context, e *row, img *Image) error {
	ext := ".png"
	if img.ContentType == TypeGIF {
		ext = ".gif"
	}
	e.key = "emoji/" + e.id.String() + ext
	if err := h.Store.Upload(ctx, e.key, bytes.NewReader(img.Data), img.ContentType, int64(len(img.Data))); err != nil {
		return fmt.Errorf("store emoji: %w", err)
	}
	if img.Still == nil {
		return nil
	}
	still := "emoji/" + e.id.String() + ".still.png"
	if err := h.Store.Upload(ctx, still, bytes.NewReader(img.Still), TypePNG, int64(len(img.Still))); err != nil {
		h.discard(ctx, e.key)
		return fmt.Errorf("store emoji still: %w", err)
	}
	e.stillKey = &still
	return nil
}

func (h *Handler) discard(ctx context.Context, keys ...string) {
	for _, k := range keys {
		if err := h.Store.Delete(context.WithoutCancel(ctx), k); err != nil {
			slog.Warn("could not delete emoji object", "key", k, "err", err)
		}
	}
}

// create handles POST /api/admin/emoji.
//
// @Summary Add a custom emoji
// @Description multipart/form-data with `name` (2 to 32 characters of a-z, 0-9 and _; upper case is folded) and `file`, a PNG, GIF or WebP of at most 256 KiB. The image is decoded and redrawn (at most 128x128, no metadata); SVG, JPEG and animated WebP are refused. Needs Manage emoji. Broadcasts emoji_changed.
// @ID createEmoji
// @Tags Emoji
// @Accept mpfd
// @Produce json
// @Security cookieAuth
// @Param name formData string true "Emoji name." minlength(2) maxlength(32)
// @Param file formData file true "PNG, GIF or WebP, at most 256 KiB."
// @Success 201 {object} Emoji "Created emoji."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Manage emoji permission, or cross-origin request rejected."
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT: the name is taken or the instance limit is reached."
// @Failure 413 {object} httpx.ErrorResponse "PAYLOAD_TOO_LARGE: body or file exceeds the limit (1 MiB for JSON)."
// @Failure 415 {object} httpx.ErrorResponse "UNSUPPORTED_MEDIA_TYPE: the file type is not allowed."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Failure 503 {object} httpx.ErrorResponse "UNAVAILABLE: a dependency (database, file storage) is not available."
// @Router /api/admin/emoji [post]
func (h *Handler) create(w http.ResponseWriter, r *http.Request) error {
	if h.Store == nil {
		return httpx.ErrUnavailable("file storage is not configured")
	}
	name, img, err := readUpload(r)
	if err != nil {
		return err
	}
	e := row{id: uuid.New(), name: name, contentType: img.ContentType, animated: img.Animated, hash: img.Hash}
	if err := h.putObjects(r.Context(), &e, img); err != nil {
		return err
	}
	if err := h.insert(r.Context(), e, len(img.Data), auth.UserFrom(r.Context()).ID); err != nil {
		h.discard(r.Context(), e.key)
		if e.stillKey != nil {
			h.discard(r.Context(), *e.stillKey)
		}
		return err
	}
	e.createdAt = time.Now().UTC()
	h.changed()
	httpx.WriteJSON(w, http.StatusCreated, e.public())
	return nil
}

// RenameRequest is the body of PATCH /api/admin/emoji/{id}.
type RenameRequest struct {
	Name string `json:"name" minLength:"2" maxLength:"32" pattern:"^[a-z0-9_]{2,32}$"`
}

// update handles PATCH /api/admin/emoji/{id}.
//
// @Summary Rename a custom emoji
// @Description Messages and reactions keep pointing at the emoji by id, so they follow the new name. Needs Manage emoji. Broadcasts emoji_changed.
// @ID renameEmoji
// @Tags Emoji
// @Accept json
// @Produce json
// @Security cookieAuth
// @Param id path string true "Emoji ID." Format(uuid)
// @Param request body RenameRequest true "Request body."
// @Success 200 {object} Emoji "Renamed emoji."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Manage emoji permission, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT: the name is taken."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/emoji/{id} [patch]
func (h *Handler) update(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	var req RenameRequest
	if err := httpx.DecodeJSON(r, &req); err != nil {
		return err
	}
	name, err := cleanName(req.Name)
	if err != nil {
		return err
	}
	e, err := h.rename(r.Context(), id, name)
	if err != nil {
		return err
	}
	h.changed()
	httpx.WriteJSON(w, http.StatusOK, e.public())
	return nil
}

// delete handles DELETE /api/admin/emoji/{id}.
//
// @Summary Delete a custom emoji
// @Description Messages and reactions that used it show `:name:` as text from now on. Needs Manage emoji. Broadcasts emoji_changed.
// @ID deleteEmoji
// @Tags Emoji
// @Security cookieAuth
// @Param id path string true "Emoji ID." Format(uuid)
// @Success 204 "Deleted."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Manage emoji permission, or cross-origin request rejected."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/emoji/{id} [delete]
func (h *Handler) delete(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	e, err := h.remove(r.Context(), id)
	if err != nil {
		return err
	}
	if h.Store != nil {
		h.discard(r.Context(), e.key)
		if e.stillKey != nil {
			h.discard(r.Context(), *e.stillKey)
		}
	}
	h.changed()
	w.WriteHeader(http.StatusNoContent)
	return nil
}
