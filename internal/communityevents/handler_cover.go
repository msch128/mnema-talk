package communityevents

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/imgproc"
)

const maxCoverBytes = 5 << 20

func readAndProcessCover(w http.ResponseWriter, r *http.Request) (*imgproc.Picture, string, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxCoverBytes+(1<<20))
	if err := r.ParseMultipartForm(maxCoverBytes); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return nil, "", httpx.NewAPIError(http.StatusRequestEntityTooLarge, httpx.CodePayloadTooLarge, "cover image exceeds 5 MB")
		}
		return nil, "", httpx.ErrInvalidInput("invalid multipart form")
	}
	defer r.MultipartForm.RemoveAll()
	file, _, err := r.FormFile("cover")
	if err != nil {
		file, _, err = r.FormFile("image")
	}
	if err != nil {
		return nil, "", httpx.ErrInvalidInput("missing cover image in form")
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, maxCoverBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("read cover: %w", err)
	}
	if int64(len(data)) > maxCoverBytes {
		return nil, "", httpx.NewAPIError(http.StatusRequestEntityTooLarge, httpx.CodePayloadTooLarge, "cover image exceeds 5 MB")
	}

	pic, err := imgproc.ProcessBanner(data)
	if errors.Is(err, imgproc.ErrImage) {
		return nil, "", httpx.ErrInvalidInput(err.Error())
	}
	if err != nil {
		return nil, "", err
	}

	ext := ".png"
	switch pic.ContentType {
	case "image/jpeg":
		ext = ".jpg"
	case "image/gif":
		ext = ".gif"
	case "image/webp":
		ext = ".webp"
	}
	return pic, ext, nil
}

// @Summary Upload event cover image
// @Description Sets the event cover image (PNG, JPEG, GIF or WebP, up to 5 MB). Crops to 3:1 ratio and strips metadata. Requires ManageEvents.
// @ID uploadCommunityEventCover
// @Tags Events
// @Accept mpfd
// @Produce json
// @Security cookieAuth
// @Param id path string true "Event ID" Format(uuid)
// @Param cover formData file true "Cover image file (at most 5 MB)"
// @Success 200 {object} CommunityEvent
// @Failure 400 {object} httpx.ErrorResponse "INVALID_INPUT"
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN"
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND"
// @Router /api/events/{id}/cover [post]
func (h *Handler) uploadCover(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	if err := h.requireManageEvents(r); err != nil {
		return err
	}
	ev, err := h.Store.Get(r.Context(), id, nil)
	if err != nil {
		return err
	}
	if err := h.requireViewable(r, ev); err != nil {
		return err
	}
	if h.MediaStore == nil {
		return httpx.ErrUnavailable("file storage is not configured")
	}

	pic, ext, err := readAndProcessCover(w, r)
	if err != nil {
		return err
	}

	user := auth.UserFrom(r.Context())
	mediaID := uuid.New()
	key := fmt.Sprintf("covers/%s%s", mediaID, ext)

	if err := h.MediaStore.Upload(r.Context(), key, bytes.NewReader(pic.Data), pic.ContentType, int64(len(pic.Data))); err != nil {
		return fmt.Errorf("upload cover storage: %w", err)
	}

	_, err = h.DB.Exec(r.Context(), `
		INSERT INTO media (id, uploader_id, channel_id, s3_bucket, s3_key, original_filename, mime_type, size_bytes)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		mediaID, user.ID, ev.ChannelID, h.MediaBucket, key, "cover"+ext, pic.ContentType, len(pic.Data),
	)
	if err != nil {
		_ = h.MediaStore.Delete(context.WithoutCancel(r.Context()), key)
		return fmt.Errorf("insert media row: %w", err)
	}

	prevID, _ := h.Store.ClearCover(r.Context(), id)
	if err := h.Store.SetCover(r.Context(), id, mediaID); err != nil {
		return err
	}
	if prevID != nil {
		h.deletePreviousMedia(r.Context(), *prevID)
	}

	updated, err := h.Store.Get(r.Context(), id, &user.ID)
	if err != nil {
		return err
	}
	h.auditRecord(r, "event.edit", updated)
	h.broadcast(updated)
	httpx.WriteJSON(w, http.StatusOK, updated)
	return nil
}

// @Summary Remove event cover image
// @Description Deletes the event cover image. Requires ManageEvents.
// @ID deleteCommunityEventCover
// @Tags Events
// @Produce json
// @Security cookieAuth
// @Param id path string true "Event ID" Format(uuid)
// @Success 200 {object} CommunityEvent
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN"
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND"
// @Router /api/events/{id}/cover [delete]
func (h *Handler) deleteCover(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	if err := h.requireManageEvents(r); err != nil {
		return err
	}
	current, err := h.Store.Get(r.Context(), id, nil)
	if err != nil {
		return err
	}
	if err := h.requireViewable(r, current); err != nil {
		return err
	}
	prevID, err := h.Store.ClearCover(r.Context(), id)
	if err != nil {
		return err
	}
	if prevID != nil {
		h.deletePreviousMedia(r.Context(), *prevID)
	}
	user := auth.UserFrom(r.Context())
	updated, err := h.Store.Get(r.Context(), id, &user.ID)
	if err != nil {
		return err
	}
	h.auditRecord(r, "event.edit", updated)
	h.broadcast(updated)
	httpx.WriteJSON(w, http.StatusOK, updated)
	return nil
}

func (h *Handler) deletePreviousMedia(ctx context.Context, mediaID uuid.UUID) {
	var key string
	if err := h.DB.QueryRow(ctx, `SELECT s3_key FROM media WHERE id = $1`, mediaID).Scan(&key); err == nil {
		_, _ = h.DB.Exec(ctx, `UPDATE media SET is_deleted = TRUE WHERE id = $1`, mediaID)
		if h.MediaStore != nil {
			_ = h.MediaStore.Delete(ctx, key)
		}
	}
}
