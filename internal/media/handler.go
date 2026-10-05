package media

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
)

const (
	MaxAvatarBytes  = 5 << 20
	maxFilenameLen  = 255
	multipartMemory = 8 << 20 // larger parts spill to a temp file
)

// Handler serves uploads, media downloads and the admin storage dashboard.
type Handler struct {
	DB             *db.Pool
	Store          Store
	Bucket         string
	Events         events.Publisher
	MaxUploadBytes int64
	// RetentionDays > 0 enables automatic pruning. Default 0: nothing is ever
	// deleted automatically.
	RetentionDays int
	// Online resolves @here in an upload's caption; nil means nobody is online.
	Online chat.OnlineSource
}

// UploadBodyLimit is the transport ceiling for upload routes: the file cap
// plus headroom for multipart framing and form fields.
func (h *Handler) UploadBodyLimit() int64 { return h.MaxUploadBytes + 1<<20 }

// MountUploads registers the multipart routes; the caller wraps them in a
// larger MaxBody than the JSON default.
func (h *Handler) MountUploads(r chi.Router) {
	r.Post("/channels/{channelID}/upload", httpx.Handle(h.upload))
	r.Post("/users/me/avatar", httpx.Handle(h.uploadAvatar))
}

// Mount registers authenticated media serving.
func (h *Handler) Mount(r chi.Router) {
	r.Get("/media/{id}", httpx.Handle(h.serve))
}

// MountAdmin registers the storage dashboard (RequireAdmin applies). Pruning is
// opt-in: the manual prune needs an explicit day count unless
// MEDIA_RETENTION_DAYS is configured.
func (h *Handler) MountAdmin(r chi.Router) {
	r.Get("/media/stats", httpx.Handle(h.stats))
	r.Get("/media", httpx.Handle(h.list))
	r.Post("/media/prune", httpx.Handle(h.prune))
	r.Delete("/media/{id}", httpx.Handle(h.delete))
}

// incomingFile is a validated upload ready to store.
type incomingFile struct {
	file     multipart.File
	filename string
	mime     string
	size     int64
}

// readFile extracts and validates the named multipart file part.
func readFile(r *http.Request, field string, maxBytes int64, allow map[string]bool, imageOnly bool) (*incomingFile, error) {
	if err := r.ParseMultipartForm(multipartMemory); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return nil, httpx.ErrPayloadTooLarge(fmt.Sprintf("file exceeds the %d MB limit", maxBytes>>20))
		}
		return nil, httpx.ErrInvalidInput("invalid multipart form")
	}
	file, header, err := r.FormFile(field)
	if err != nil {
		return nil, httpx.ErrInvalidInput(field + " file is required")
	}
	if header.Size > maxBytes {
		file.Close()
		return nil, httpx.ErrPayloadTooLarge(fmt.Sprintf("file exceeds the %d MB limit", maxBytes>>20))
	}
	if header.Size == 0 {
		file.Close()
		return nil, httpx.ErrInvalidInput("file is empty")
	}

	head := make([]byte, 512)
	n, err := io.ReadFull(file, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		file.Close()
		return nil, fmt.Errorf("read upload head: %w", err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		file.Close()
		return nil, fmt.Errorf("rewind upload: %w", err)
	}

	filename := httpx.SanitizeFilename(header.Filename, maxFilenameLen)
	if filename == "" {
		filename = "file"
	}
	mimeType, err := reconcileMIME(declaredMIME(header.Header.Get("Content-Type"), filename), head[:n], allow, imageOnly)
	if err != nil {
		file.Close()
		return nil, httpx.ErrUnsupportedMediaType(err.Error())
	}
	return &incomingFile{file: file, filename: filename, mime: mimeType, size: header.Size}, nil
}

func storageKey(prefix string, id uuid.UUID, mimeType string) string {
	now := time.Now().UTC()
	return fmt.Sprintf("%s/%04d/%02d/%s%s", prefix, now.Year(), now.Month(), id, extensionFor(mimeType))
}

// upload handles POST /api/channels/{channelID}/upload.
//
// @Summary Post a message with an attachment
// @Description multipart/form-data. Creates a message carrying the file and broadcasts message_create. Not allowed in voice channels. Stricter rate limit (30 per minute).
// @ID uploadFile
// @Tags Media
// @Accept mpfd
// @Produce json
// @Security cookieAuth
// @Param channelID path string true "Channel ID." Format(uuid)
// @Param file formData file true "At most MAX_UPLOAD_MB (config); type is sniffed and checked against an allowlist."
// @Param content formData string false "Optional caption." maxlength(4000)
// @Param parent_id formData string false "Thread root message ID (reply in a thread)." Format(uuid)
// @Param reply_to_id formData string false "Quoted message ID." Format(uuid)
// @Success 201 {object} chat.Message "Created message."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: not allowed (admin required, not the author, wrong current password) or cross-origin request rejected by the CSRF check."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 413 {object} httpx.ErrorResponse "PAYLOAD_TOO_LARGE: body or file exceeds the limit (1 MiB for JSON)."
// @Failure 415 {object} httpx.ErrorResponse "UNSUPPORTED_MEDIA_TYPE: the file type is not allowed."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Failure 503 {object} httpx.ErrorResponse "UNAVAILABLE: a dependency (database, file storage) is not available."
// @Router /api/channels/{channelID}/upload [post]
func (h *Handler) upload(w http.ResponseWriter, r *http.Request) error {
	if h.Store == nil {
		return httpx.ErrUnavailable("file storage is not configured")
	}
	user := auth.UserFrom(r.Context())
	chID, err := httpx.PathUUID(r, "channelID")
	if err != nil {
		return err
	}
	if _, err := chat.TextChannel(r.Context(), h.DB, chID); err != nil {
		return err
	}

	in, err := readFile(r, "file", h.MaxUploadBytes, allowedMIME, false)
	if err != nil {
		return err
	}
	defer in.file.Close()

	content, err := chat.ValidateContent(r.FormValue("content"), true)
	if err != nil {
		return err
	}
	var parentID *uuid.UUID
	if raw := r.FormValue("parent_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			return httpx.ErrInvalidInput("invalid parent_id")
		}
		parentID = &id
	}
	var replyToID *uuid.UUID
	if raw := r.FormValue("reply_to_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			return httpx.ErrInvalidInput("invalid reply_to_id")
		}
		replyToID = &id
	}
	if err := chat.ValidateTarget(r.Context(), h.DB, chID, parentID, replyToID); err != nil {
		return err
	}

	mediaID := uuid.New()
	key := storageKey("uploads", mediaID, in.mime)
	if err := h.Store.Upload(r.Context(), key, in.file, in.mime, in.size); err != nil {
		return fmt.Errorf("store upload: %w", err)
	}

	msgID, err := chat.InsertMessage(r.Context(), h.DB, h.Online, chat.NewMessage{
		ChannelID: chID, UserID: user.ID, Content: content, ParentID: parentID, ReplyToID: replyToID,
	}, func(tx pgx.Tx, msgID uuid.UUID) error {
		_, err := tx.Exec(r.Context(), `
			INSERT INTO media (id, uploader_id, channel_id, message_id, s3_bucket, s3_key, original_filename, mime_type, size_bytes)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			mediaID, user.ID, chID, msgID, h.Bucket, key, in.filename, in.mime, in.size)
		return err
	})
	if err != nil {
		// The object must not outlive a failed DB write.
		if delErr := h.Store.Delete(context.WithoutCancel(r.Context()), key); delErr != nil {
			slog.Warn("orphaned upload after failed insert", "key", key, "err", delErr)
		}
		return fmt.Errorf("record upload: %w", err)
	}
	return chat.PublishMessage(w, r, h.DB, h.Events, msgID)
}

// uploadAvatar handles POST /api/users/me/avatar.
//
// @Summary Upload avatar
// @Description multipart/form-data with an image in field 'avatar' (max 5 MB). Replaces and deletes the previous avatar. Broadcasts user_update.
// @ID uploadAvatar
// @Tags Users
// @Accept mpfd
// @Produce json
// @Security cookieAuth
// @Param avatar formData file true "Image file, at most 5 MB."
// @Success 200 {object} auth.User "Updated user."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: not allowed (admin required, not the author, wrong current password) or cross-origin request rejected by the CSRF check."
// @Failure 413 {object} httpx.ErrorResponse "PAYLOAD_TOO_LARGE: body or file exceeds the limit (1 MiB for JSON)."
// @Failure 415 {object} httpx.ErrorResponse "UNSUPPORTED_MEDIA_TYPE: the file type is not allowed."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Failure 503 {object} httpx.ErrorResponse "UNAVAILABLE: a dependency (database, file storage) is not available."
// @Router /api/users/me/avatar [post]
func (h *Handler) uploadAvatar(w http.ResponseWriter, r *http.Request) error {
	if h.Store == nil {
		return httpx.ErrUnavailable("file storage is not configured")
	}
	user := auth.UserFrom(r.Context())
	in, err := readFile(r, "avatar", MaxAvatarBytes, avatarMIME, true)
	if err != nil {
		return err
	}
	defer in.file.Close()

	mediaID := uuid.New()
	key := storageKey("avatars", mediaID, in.mime)
	if err := h.Store.Upload(r.Context(), key, in.file, in.mime, in.size); err != nil {
		return fmt.Errorf("store avatar: %w", err)
	}

	var previous *string
	err = pgx.BeginFunc(r.Context(), h.DB, func(tx pgx.Tx) error {
		if err := tx.QueryRow(r.Context(), `SELECT avatar_s3_key FROM users WHERE id = $1 FOR UPDATE`, user.ID).Scan(&previous); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), `
			INSERT INTO media (id, uploader_id, s3_bucket, s3_key, original_filename, mime_type, size_bytes)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			mediaID, user.ID, h.Bucket, key, in.filename, in.mime, in.size); err != nil {
			return err
		}
		_, err := tx.Exec(r.Context(), `UPDATE users SET avatar_s3_key = $1, updated_at = NOW() WHERE id = $2`, mediaID.String(), user.ID)
		return err
	})
	if err != nil {
		_ = h.Store.Delete(context.WithoutCancel(r.Context()), key)
		return fmt.Errorf("record avatar: %w", err)
	}

	// The replaced avatar is no longer referenced anywhere; remove it.
	if previous != nil {
		if oldID, err := uuid.Parse(*previous); err == nil {
			if err := DeleteMedia(context.WithoutCancel(r.Context()), h.DB, h.Store, oldID); err != nil {
				slog.Warn("could not delete previous avatar", "media_id", oldID, "err", err)
			}
		}
	}

	updated, err := auth.GetUser(r.Context(), h.DB, user.ID)
	if err != nil {
		return err
	}
	h.Events.Broadcast("user_update", updated.Public())
	httpx.WriteJSON(w, http.StatusOK, updated)
	return nil
}

// serve streams a media object to an authenticated member.
//
// @Summary Download a media object
// @Description Streams an attachment or avatar. Supports Range and conditional requests. Inline for safe types, attachment otherwise.
// @ID getMedia
// @Tags Media
// @Produce octet-stream,json
// @Security cookieAuth
// @Param id path string true "Resource ID." Format(uuid)
// @Success 200 {string} string "File content; Content-Type is the stored MIME type."
// @Success 206 {string} string "Partial content for a Range request."
// @Success 304 "Not modified (If-None-Match)."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 410 {object} httpx.ErrorResponse "Media expired or was deleted. The error code is NOT_FOUND."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Failure 503 {object} httpx.ErrorResponse "UNAVAILABLE: a dependency (database, file storage) is not available."
// @Router /api/media/{id} [get]
func (h *Handler) serve(w http.ResponseWriter, r *http.Request) error {
	if h.Store == nil {
		return httpx.ErrUnavailable("file storage is not configured")
	}
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	var key, mimeType, filename string
	var size int64
	var deleted bool
	err = h.DB.QueryRow(r.Context(), `
		SELECT s3_key, mime_type, original_filename, size_bytes, is_deleted FROM media WHERE id = $1`, id).
		Scan(&key, &mimeType, &filename, &size, &deleted)
	if errors.Is(err, pgx.ErrNoRows) {
		return httpx.ErrNotFound("media not found")
	}
	if err != nil {
		return err
	}
	if deleted {
		return httpx.NewAPIError(http.StatusGone, httpx.CodeNotFound, "media has expired or was deleted")
	}

	obj := &objectReader{ctx: r.Context(), store: h.Store, key: key, size: size}
	defer obj.Close()

	disposition := "attachment"
	if inlineMIME[mimeType] {
		disposition = "inline"
	}
	hdr := w.Header()
	hdr.Set("Content-Type", mimeType)
	hdr.Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": filename}))
	hdr.Set("X-Content-Type-Options", "nosniff")
	hdr.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	// Content behind an ID never changes; private keeps shared caches out.
	hdr.Set("Cache-Control", "private, max-age=86400, immutable")
	hdr.Set("ETag", `"`+id.String()+`"`)
	// ServeContent answers Range (206, video seeking), If-None-Match and HEAD.
	http.ServeContent(w, r, "", time.Time{}, obj)
	return nil
}

// stats handles GET /api/admin/media/stats.
//
// @Summary Storage totals
// @Description Requires role admin (403 otherwise).
// @ID getMediaStats
// @Tags Admin
// @Produce json
// @Security cookieAuth
// @Success 200 {object} Stats "Totals."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: not allowed (admin required, not the author, wrong current password) or cross-origin request rejected by the CSRF check."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/media/stats [get]
func (h *Handler) stats(w http.ResponseWriter, r *http.Request) error {
	stats, err := GetStats(r.Context(), h.DB)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, stats)
	return nil
}

// list handles GET /api/admin/media.
//
// @Summary List all media, newest first
// @Description Requires role admin (403 otherwise).
// @ID listMedia
// @Tags Admin
// @Produce json
// @Security cookieAuth
// @Param limit query int false "Page size. Values below 1 or non-numeric fall back to the default; larger values are clamped to 100." minimum(1) maximum(100) default(50)
// @Param offset query int false "Number of items to skip." minimum(0) default(0)
// @Success 200 {array} DashboardItem "Media page."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: not allowed (admin required, not the author, wrong current password) or cross-origin request rejected by the CSRF check."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/media [get]
func (h *Handler) list(w http.ResponseWriter, r *http.Request) error {
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	if offset < 0 {
		offset = 0
	}
	items, err := ListMedia(r.Context(), h.DB, httpx.QueryLimit(r, "limit", 50, 100), offset)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, items)
	return nil
}

// prune handles POST /api/admin/media/prune.
//
// @Summary Delete old chat attachments
// @Description Avatars are never pruned. Without days, uses MEDIA_RETENTION_DAYS; 400 when that is disabled. Requires role admin (403 otherwise).
// @ID pruneMedia
// @Tags Admin
// @Produce json
// @Security cookieAuth
// @Param days query int false "Delete attachments older than this many days; defaults to MEDIA_RETENTION_DAYS." minimum(1) maximum(3650)
// @Success 200 {object} PruneResult "Prune result."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: not allowed (admin required, not the author, wrong current password) or cross-origin request rejected by the CSRF check."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Failure 503 {object} httpx.ErrorResponse "UNAVAILABLE: a dependency (database, file storage) is not available."
// @Router /api/admin/media/prune [post]
func (h *Handler) prune(w http.ResponseWriter, r *http.Request) error {
	if h.Store == nil {
		return httpx.ErrUnavailable("file storage is not configured")
	}
	days := h.RetentionDays
	if raw := r.URL.Query().Get("days"); raw != "" {
		d, err := strconv.Atoi(raw)
		if err != nil || d < 1 || d > 3650 {
			return httpx.ErrInvalidInput("days must be between 1 and 3650")
		}
		days = d
	}
	if days < 1 {
		return httpx.ErrInvalidInput("retention is disabled; pass days explicitly")
	}
	n, err := PruneOlderThan(r.Context(), h.DB, h.Store, days)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, PruneResult{PrunedCount: n, CutoffDays: days})
	return nil
}

// delete handles DELETE /api/admin/media/{id}.
//
// @Summary Delete a media object
// @Description Removes the object from storage and marks it deleted; chat shows a placeholder. Idempotent. Requires role admin (403 otherwise).
// @ID deleteMedia
// @Tags Admin
// @Produce json
// @Security cookieAuth
// @Param id path string true "Resource ID." Format(uuid)
// @Success 204 "Success, no content."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: not allowed (admin required, not the author, wrong current password) or cross-origin request rejected by the CSRF check."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Failure 503 {object} httpx.ErrorResponse "UNAVAILABLE: a dependency (database, file storage) is not available."
// @Router /api/admin/media/{id} [delete]
func (h *Handler) delete(w http.ResponseWriter, r *http.Request) error {
	if h.Store == nil {
		return httpx.ErrUnavailable("file storage is not configured")
	}
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	if err := DeleteMedia(r.Context(), h.DB, h.Store, id); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// PruneResult is the response of POST /api/admin/media/prune.
type PruneResult struct {
	PrunedCount int `json:"pruned_count"`
	CutoffDays  int `json:"cutoff_days"`
}
