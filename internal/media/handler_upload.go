package media

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// incomingFile is a validated upload ready to store.
type incomingFile struct {
	file     multipart.File
	filename string
	mime     string
	size     int64
}

type readFileOptions struct {
	field     string
	maxBytes  int64
	allow     map[string]bool
	imageOnly bool
}

func parseMultipartForm(r *http.Request, maxBytes int64) error {
	if err := r.ParseMultipartForm(multipartMemory); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return httpx.ErrPayloadTooLarge(fmt.Sprintf("file exceeds the %d MB limit", maxBytes>>20))
		}
		return httpx.ErrInvalidInput("invalid multipart form")
	}
	return nil
}

func validateFileSize(file multipart.File, size, maxBytes int64) error {
	if size > maxBytes {
		file.Close()
		return httpx.ErrPayloadTooLarge(fmt.Sprintf("file exceeds the %d MB limit", maxBytes>>20))
	}
	if size == 0 {
		file.Close()
		return httpx.ErrInvalidInput("file is empty")
	}
	return nil
}

func readUploadHead(file multipart.File) ([]byte, error) {
	head := make([]byte, 512)
	n, err := io.ReadFull(file, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, fmt.Errorf("read upload head: %w", err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("rewind upload: %w", err)
	}
	return head[:n], nil
}

// readFile extracts and validates the named multipart file part.
func readFile(r *http.Request, opts readFileOptions) (*incomingFile, error) {
	if err := parseMultipartForm(r, opts.maxBytes); err != nil {
		return nil, err
	}
	file, header, err := r.FormFile(opts.field)
	if err != nil {
		return nil, httpx.ErrInvalidInput(opts.field + " file is required")
	}
	if err := validateFileSize(file, header.Size, opts.maxBytes); err != nil {
		return nil, err
	}
	head, err := readUploadHead(file)
	if err != nil {
		file.Close()
		return nil, err
	}
	filename := httpx.SanitizeFilename(header.Filename, maxFilenameLen)
	if filename == "" {
		filename = "file"
	}
	mimeType, err := reconcileMIME(declaredMIME(header.Header.Get("Content-Type"), filename), head, opts.allow, opts.imageOnly)
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

func (h *Handler) prepareUploadRequest(w http.ResponseWriter, r *http.Request, user *auth.User) (*http.Request, error) {
	limit, err := h.memberLimit(r.Context(), user.Subject())
	if err != nil {
		return nil, err
	}
	// The route's body cap is the ceiling's; this request gets its own.
	if r.ContentLength > bodyLimit(limit) {
		return nil, httpx.ErrPayloadTooLarge(fmt.Sprintf("file exceeds the %d MB limit", limit>>20))
	}
	r.Body = http.MaxBytesReader(w, r.Body, bodyLimit(limit))
	return r.WithContext(WithUploadLimit(r.Context(), limit)), nil
}

func (h *Handler) insertUploadMessage(ctx context.Context, chID uuid.UUID, user *auth.User, in *storedUpload, target *uploadTarget) (uuid.UUID, error) {
	nm := chat.NewMessage{
		ChannelID: chID, UserID: user.ID, Content: target.content, ParentID: target.parentID, ReplyToID: target.replyToID, HasAttachment: true,
	}
	var storePost func(tx pgx.Tx, msgID uuid.UUID) error
	if target.post != nil {
		nm.Title = &target.post.Title
		storePost = target.post.Store(ctx, user.ID)
	}
	msgID, err := chat.InsertMessage(ctx, h.DB, h.Online, nm, func(tx pgx.Tx, msgID uuid.UUID) error {
		if storePost != nil {
			if err := storePost(tx, msgID); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO media (id, uploader_id, channel_id, message_id, s3_bucket, s3_key, original_filename, mime_type, size_bytes)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			in.id, user.ID, chID, msgID, h.Bucket, in.key, in.filename, in.mime, in.size)
		return err
	})
	if err != nil {
		// The object must not outlive a failed DB write.
		h.discard(ctx, in.key)
		return uuid.Nil, fmt.Errorf("record upload: %w", err)
	}
	return msgID, nil
}

// upload handles POST /api/channels/{channelID}/upload.
//
// @Summary Post a message with an attachment
// @Description multipart/form-data. Creates a message carrying the file and broadcasts message_create. Works in text and voice channels. Stricter rate limit (30 per minute).
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
// @Param title formData string false "In a forum channel: makes the message a post with this title (1 to 100 characters; not with parent_id or reply_to_id). A root message in a forum needs one." maxlength(100)
// @Param tag_ids formData string false "With title: the post's tags, comma-separated IDs (at most 5, the forum's rules for moderated and required tags apply)."
// @Success 201 {object} chat.Message "Created message."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: missing the Send messages or Attach files permission in the channel, or cross-origin request rejected. RULES_NOT_ACCEPTED: the member has not accepted the community rules yet."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 413 {object} httpx.ErrorResponse "PAYLOAD_TOO_LARGE: body or file exceeds the limit (1 MiB for JSON)."
// @Failure 415 {object} httpx.ErrorResponse "UNSUPPORTED_MEDIA_TYPE: the file type is not allowed."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Failure 503 {object} httpx.ErrorResponse "UNAVAILABLE: a dependency (database, file storage) is not available."
// @Router /api/channels/{channelID}/upload [post]
func (h *Handler) upload(w http.ResponseWriter, r *http.Request) error {
	user := auth.UserFrom(r.Context())
	chID, err := httpx.PathUUID(r, "channelID")
	if err != nil {
		return err
	}
	ctx, err := chat.UploadContext(h.Perms, r, chID)
	if err != nil {
		return err
	}
	if _, err := chat.ChannelForMessages(ctx, h.DB, chID); err != nil {
		return err
	}
	req, err := h.prepareUploadRequest(w, r, user)
	if err != nil {
		return err
	}
	// The body is capped at the route's limit, so the reservation is an upper
	// bound of what is stored.
	release, err := h.ReserveUpload(req.Context(), user.ID, AnnouncedSize(req, bodyLimit(h.limit(req.Context()))))
	if err != nil {
		return err
	}
	defer release()
	in, target, err := h.readUpload(req, chID)
	if err != nil {
		return err
	}
	msgID, err := h.insertUploadMessage(ctx, chID, user, in, target)
	if err != nil {
		return err
	}
	return chat.PublishMessage(w, req, h.DB, h.Events, msgID)
}

// uploadAvatar handles POST /api/users/me/avatar.
//
// @Summary Upload avatar
// @Description multipart/form-data with an image in field 'avatar' (PNG, JPEG, GIF or WebP, max 5 MB). The server never stores the upload: it crops the centre square, shrinks it to at most 512x512 (never enlarged) and writes a new file, so metadata such as EXIF (GPS, camera model) is gone. An animated GIF stays animated within a budget of 16 million pixels (canvas times frames, at most 100 frames); animated WebP is rejected. Replaces and deletes the previous avatar. Broadcasts user_update.
// @ID uploadAvatar
// @Tags Users
// @Accept mpfd
// @Produce json
// @Security cookieAuth
// @Param avatar formData file true "Image file, at most 5 MB."
// @Success 200 {object} auth.User "Updated user."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): no file, too many frames, too large in pixels, or the picture cannot be read."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: cross-origin request rejected by the CSRF check."
// @Failure 413 {object} httpx.ErrorResponse "PAYLOAD_TOO_LARGE: body or file exceeds the limit (1 MiB for JSON)."
// @Failure 415 {object} httpx.ErrorResponse "UNSUPPORTED_MEDIA_TYPE: the file type is not allowed."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Failure 503 {object} httpx.ErrorResponse "UNAVAILABLE: a dependency (database, file storage) is not available."
// @Router /api/users/me/avatar [post]
func (h *Handler) uploadAvatar(w http.ResponseWriter, r *http.Request) error {
	user := auth.UserFrom(r.Context())
	if err := h.ReplaceAvatar(r, user.ID); err != nil {
		return err
	}
	updated, err := auth.GetUser(r.Context(), h.DB, user.ID)
	if err != nil {
		return err
	}
	h.Events.Broadcast(events.UserUpdate, updated.Public())
	httpx.WriteJSON(w, http.StatusOK, updated)
	return nil
}

// deletePreviousImage deletes the object of a replaced or removed avatar or banner.
func (h *Handler) deletePreviousImage(ctx context.Context, previous *string) {
	if previous == nil {
		return
	}
	oldID, err := uuid.Parse(*previous)
	if err != nil {
		return
	}
	if err := DeleteMedia(context.WithoutCancel(ctx), h.DB, h.Store, oldID); err != nil {
		slog.Warn("could not delete previous profile image", "media_id", oldID, "err", err)
	}
}

// ReplaceAvatar reads the 'avatar' image of a multipart request (at most
// MaxAvatarBytes, image types only), redraws it as a square of at most 512
// pixels without metadata, makes it userID's avatar and deletes the previous
// one. Webhooks and bots use it for their account.
func (h *Handler) ReplaceAvatar(r *http.Request, userID uuid.UUID) error {
	if h.Store == nil {
		return httpx.ErrUnavailable("file storage is not configured")
	}
	pic, err := avatarImage.read(r)
	if err != nil {
		return err
	}
	return h.setProfileImage(r.Context(), avatarImage, userID, pic)
}

// deleteAvatar handles DELETE /api/users/me/avatar.
//
// @Summary Remove avatar
// @Description Deletes the avatar and its stored file. Succeeds when there is none. Needs the profile.avatar_remove feature. Broadcasts user_update.
// @ID deleteAvatar
// @Tags Users
// @Produce json
// @Security cookieAuth
// @Success 200 {object} auth.User "Updated user, without avatar_url."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: cross-origin request rejected by the CSRF check."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Failure 503 {object} httpx.ErrorResponse "UNAVAILABLE: a dependency (database, file storage) is not available."
// @Router /api/users/me/avatar [delete]
func (h *Handler) deleteAvatar(w http.ResponseWriter, r *http.Request) error {
	user := auth.UserFrom(r.Context())
	previous, err := h.clearProfileImage(r.Context(), avatarImage, user.ID)
	if err != nil {
		return err
	}
	h.deletePreviousImage(r.Context(), previous)
	updated, err := auth.GetUser(r.Context(), h.DB, user.ID)
	if err != nil {
		return err
	}
	h.Events.Broadcast(events.UserUpdate, updated.Public())
	httpx.WriteJSON(w, http.StatusOK, updated)
	return nil
}
