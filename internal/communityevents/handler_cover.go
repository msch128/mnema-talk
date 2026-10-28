package communityevents

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/imgproc"
	"github.com/msch128/mnema-talk/internal/media"
)

const maxCoverBytes = 5 << 20

// readCover reads the cover picture of the request (field "cover", or "image")
// with the checks every uploaded picture gets, and crops it to the banner ratio.
func readCover(w http.ResponseWriter, r *http.Request) (*imgproc.Picture, error) {
	r.Body = http.MaxBytesReader(w, r.Body, CoverBodyLimit)
	return media.ReadPicture(r, media.PictureUpload{Fields: []string{"cover", "image"}, MaxBytes: maxCoverBytes, Process: imgproc.ProcessBanner})
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
// @Failure 413 {object} httpx.ErrorResponse "PAYLOAD_TOO_LARGE"
// @Failure 415 {object} httpx.ErrorResponse "UNSUPPORTED_MEDIA_TYPE"
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

	pic, err := readCover(w, r)
	if err != nil {
		return err
	}
	ext := media.ExtensionFor(pic.ContentType)

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

	prevID, err := h.Store.ReplaceCover(r.Context(), id, mediaID)
	if err != nil {
		// The new cover is not used: drop its row and object.
		h.deleteMedia(r.Context(), mediaID)
		return err
	}
	if prevID != nil {
		h.deleteMedia(r.Context(), *prevID)
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
		h.deleteMedia(r.Context(), *prevID)
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

// deleteMedia removes a cover's object and row. It runs to the end when the
// request goes away, and only logs a failure: the event is already updated.
func (h *Handler) deleteMedia(ctx context.Context, mediaID uuid.UUID) {
	if h.MediaStore == nil {
		return
	}
	if err := media.DeleteMedia(context.WithoutCancel(ctx), h.DB, h.MediaStore, mediaID); err != nil {
		slog.WarnContext(ctx, "could not delete event cover", "media_id", mediaID, "err", err)
	}
}
