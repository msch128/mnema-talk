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

func (h *Handler) upload(w http.ResponseWriter, r *http.Request) error {
	if h.Store == nil {
		return httpx.ErrUnavailable("file storage is not configured")
	}
	user := auth.UserFrom(r.Context())
	chID, err := httpx.PathUUID(r, "channelID")
	if err != nil {
		return err
	}
	ch, err := chat.LoadChannel(r.Context(), h.DB, chID)
	if err != nil {
		return err
	}
	if ch.Type == chat.ChannelTypeVoice {
		return httpx.ErrInvalidInput("voice channels have no text chat")
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
	if err := chat.ValidateParent(r.Context(), h.DB, chID, parentID); err != nil {
		return err
	}
	var replyToID *uuid.UUID
	if raw := r.FormValue("reply_to_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			return httpx.ErrInvalidInput("invalid reply_to_id")
		}
		replyToID = &id
	}
	if err := chat.ValidateReplyTarget(r.Context(), h.DB, chID, parentID, replyToID); err != nil {
		return err
	}

	mediaID := uuid.New()
	key := storageKey("uploads", mediaID, in.mime)
	if err := h.Store.Upload(r.Context(), key, in.file, in.mime, in.size); err != nil {
		return fmt.Errorf("store upload: %w", err)
	}

	var msgID uuid.UUID
	err = pgx.BeginFunc(r.Context(), h.DB, func(tx pgx.Tx) error {
		var err error
		if msgID, err = chat.CreateMessage(r.Context(), tx, chat.NewMessage{
			ChannelID: chID, UserID: user.ID, Content: content, ParentID: parentID, ReplyToID: replyToID,
		}); err != nil {
			return err
		}
		if err := chat.RecordMentions(r.Context(), tx, h.Online, msgID, user.ID, content); err != nil {
			return err
		}
		_, err = tx.Exec(r.Context(), `
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

	msg, err := chat.GetMessage(r.Context(), h.DB, msgID)
	if err != nil {
		return err
	}
	h.Events.Broadcast("message_create", msg)
	httpx.WriteJSON(w, http.StatusCreated, msg)
	return nil
}

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

func (h *Handler) stats(w http.ResponseWriter, r *http.Request) error {
	stats, err := GetStats(r.Context(), h.DB)
	if err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, stats)
	return nil
}

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
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"pruned_count": n, "cutoff_days": days})
	return nil
}

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
