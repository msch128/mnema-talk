package wiki

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/imgproc"
	"github.com/msch128/mnema-talk/internal/media"
	"github.com/msch128/mnema-talk/internal/perm"
)

const (
	// MaxImageBytes is the largest picture accepted (before it is redrawn).
	MaxImageBytes = 5 << 20
	// ImageBodyLimit is the body ceiling of the upload: the picture plus the form around it.
	ImageBodyLimit = MaxImageBytes + 1<<20
	// MaxImages is how many pictures the wiki holds. At the cap, pictures no
	// revision uses (a day old at least) make room; else the upload is refused.
	MaxImages = 200
	// imageGrace is how long a new picture may stay unused: its page is still being written.
	imageGrace = 24 * time.Hour
	// imageSweep is how many unused pictures one upload removes.
	imageSweep = 20
)

var errTooManyImages = httpx.ErrConflict("the wiki holds at most 200 pictures")

// Image is an uploaded picture: put the markdown in the page.
type Image struct {
	ID       uuid.UUID `json:"id" format:"uuid"`
	URL      string    `json:"url"`
	Markdown string    `json:"markdown"`
	Size     int       `json:"size"`
}

// MountUploads registers the multipart picture upload; the caller wraps it in a
// MaxBody of ImageBodyLimit instead of the JSON default.
func (h *Handler) MountUploads(r chi.Router) {
	if h.WriteLimit != nil {
		r = r.With(h.WriteLimit.By(auth.UserKey))
	}
	r.Post("/wiki/images", httpx.Handle(h.uploadImage))
}

// @Summary Upload a wiki picture
// @Description Stores a picture for use in wiki pages (PNG, JPEG, GIF or WebP, up to 5 MB). The picture is redrawn without metadata and shrunk to 1600 pixels. Put the returned markdown in a page. Requires EditWiki.
// @ID uploadWikiImage
// @Tags Wiki
// @Accept mpfd
// @Produce json
// @Security cookieAuth
// @Param image formData file true "Picture file (at most 5 MB)"
// @Success 201 {object} Image
// @Failure 400 {object} httpx.ErrorResponse "INVALID_INPUT"
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN"
// @Failure 409 {object} httpx.ErrorResponse "CONFLICT"
// @Failure 413 {object} httpx.ErrorResponse "PAYLOAD_TOO_LARGE"
// @Failure 415 {object} httpx.ErrorResponse "UNSUPPORTED_MEDIA_TYPE"
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED"
// @Router /api/wiki/images [post]
func (h *Handler) uploadImage(w http.ResponseWriter, r *http.Request) error {
	if err := h.require(r, perm.EditWiki); err != nil {
		return err
	}
	if h.MediaStore == nil {
		return httpx.ErrUnavailable("file storage is not configured")
	}
	r.Body = http.MaxBytesReader(w, r.Body, ImageBodyLimit)
	pic, err := media.ReadPicture(r, media.PictureUpload{Fields: []string{"image"}, MaxBytes: MaxImageBytes, Process: imgproc.ProcessInline})
	if err != nil {
		return err
	}
	if err := h.Store.makeRoom(r.Context(), h.MediaStore); err != nil {
		return err
	}
	img, err := h.storeImage(r.Context(), auth.UserFrom(r.Context()).ID, pic)
	if err != nil {
		return err
	}
	h.recordImage(r, img)
	httpx.WriteJSON(w, http.StatusCreated, img)
	return nil
}

// storeImage writes the object and its rows; none of them is left behind when one step fails.
func (h *Handler) storeImage(ctx context.Context, user uuid.UUID, pic *imgproc.Picture) (Image, error) {
	id := uuid.New()
	ext := media.ExtensionFor(pic.ContentType)
	key := fmt.Sprintf("wiki/%s%s", id, ext)
	if err := h.MediaStore.Upload(ctx, key, bytes.NewReader(pic.Data), pic.ContentType, int64(len(pic.Data))); err != nil {
		return Image{}, fmt.Errorf("upload wiki picture: %w", err)
	}
	err := pgx.BeginFunc(ctx, h.Store.DB, func(q pgx.Tx) error {
		if _, err := q.Exec(ctx, `
			INSERT INTO media (id, uploader_id, s3_bucket, s3_key, original_filename, mime_type, size_bytes)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`, id, user, h.MediaBucket, key, "wiki"+ext, pic.ContentType, len(pic.Data)); err != nil {
			return fmt.Errorf("insert wiki picture: %w", err)
		}
		_, err := q.Exec(ctx, `INSERT INTO wiki_images (media_id) VALUES ($1)`, id)
		return err
	})
	if err != nil {
		_ = h.MediaStore.Delete(context.WithoutCancel(ctx), key)
		return Image{}, err
	}
	url := "/api/media/" + id.String()
	return Image{ID: id, URL: url, Markdown: "![](" + url + ")", Size: len(pic.Data)}, nil
}

func (h *Handler) recordImage(r *http.Request, img Image) {
	if h.Audit == nil {
		return
	}
	h.Audit.RecordOrLog(r.Context(), audit.Entry{
		Action: "wiki.image", TargetType: "wiki_image", TargetID: img.ID.String(), TargetName: "wiki picture",
	})
}

// makeRoom refuses another picture at the cap, after removing the pictures no
// revision refers to (older than a day, oldest first).
func (s *Store) makeRoom(ctx context.Context, store media.Store) error {
	var n int
	if err := s.DB.QueryRow(ctx, `SELECT count(*) FROM wiki_images`).Scan(&n); err != nil {
		return err
	}
	if n < MaxImages {
		return nil
	}
	rows, err := s.DB.Query(ctx, `
		SELECT wi.media_id FROM wiki_images wi
		WHERE wi.created_at < $1
		  AND NOT EXISTS (SELECT 1 FROM wiki_revisions r WHERE strpos(r.content, '/api/media/' || wi.media_id::text) > 0)
		ORDER BY wi.created_at LIMIT $2`, time.Now().Add(-imageGrace), imageSweep)
	if err != nil {
		return fmt.Errorf("find unused wiki pictures: %w", err)
	}
	unused, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return err
	}
	for _, id := range unused {
		if err := media.DeleteMedia(ctx, s.DB, store, id); err != nil {
			slog.WarnContext(ctx, "could not delete an unused wiki picture", "media_id", id, "err", err)
			continue
		}
		if _, err := s.DB.Exec(ctx, `DELETE FROM wiki_images WHERE media_id = $1`, id); err != nil {
			return err
		}
	}
	if len(unused) == 0 {
		return errTooManyImages
	}
	return nil
}
