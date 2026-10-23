package media

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/imgproc"
)

// MaxBannerBytes is the size of an uploaded banner and of the stored result.
const MaxBannerBytes = 5 << 20

// putBanner handles PUT /api/users/me/banner.
//
// @Summary Set the profile banner
// @Description multipart/form-data with an image in field 'banner' (PNG, JPEG, GIF or WebP, at most 5 MB, at least 300x100 pixels). The server never stores the upload: it decodes it, crops the centre 3:1, shrinks it to at most 1500x500 and writes a new file, so metadata such as EXIF is gone. An animated GIF stays animated within a budget of 16 million pixels (canvas times frames, at most 100 frames); animated WebP is rejected. Replaces and deletes the previous banner. Needs the profile.banner feature. Same rate limit as other uploads.
// @ID setBanner
// @Tags Users
// @Accept mpfd
// @Produce json
// @Security cookieAuth
// @Param banner formData file true "Image file, at most 5 MB."
// @Success 200 {object} auth.User "Updated user, including banner_url."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): no file, too small or too large in pixels, too many frames, or the picture cannot be read."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: cross-origin request rejected by the CSRF check."
// @Failure 413 {object} httpx.ErrorResponse "PAYLOAD_TOO_LARGE: body or file exceeds the limit (1 MiB for JSON)."
// @Failure 415 {object} httpx.ErrorResponse "UNSUPPORTED_MEDIA_TYPE: the file type is not allowed."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Failure 503 {object} httpx.ErrorResponse "UNAVAILABLE: a dependency (database, file storage) is not available."
// @Router /api/users/me/banner [put]
func (h *Handler) putBanner(w http.ResponseWriter, r *http.Request) error {
	if h.Store == nil {
		return httpx.ErrUnavailable("file storage is not configured")
	}
	user := auth.UserFrom(r.Context())
	banner, err := readBanner(r)
	if err != nil {
		return err
	}
	if err := h.replaceBanner(r.Context(), user.ID, banner); err != nil {
		return err
	}
	return h.writeBannerUser(w, r, user.ID)
}

// deleteBanner handles DELETE /api/users/me/banner.
//
// @Summary Remove the profile banner
// @Description Deletes the banner and its stored file. Succeeds when there is none. Needs the profile.banner feature.
// @ID deleteBanner
// @Tags Users
// @Produce json
// @Security cookieAuth
// @Success 200 {object} auth.User "Updated user, without banner_url."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: cross-origin request rejected by the CSRF check."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Failure 503 {object} httpx.ErrorResponse "UNAVAILABLE: a dependency (database, file storage) is not available."
// @Router /api/users/me/banner [delete]
func (h *Handler) deleteBanner(w http.ResponseWriter, r *http.Request) error {
	if h.Store == nil {
		return httpx.ErrUnavailable("file storage is not configured")
	}
	user := auth.UserFrom(r.Context())
	previous, err := h.clearBanner(r.Context(), user.ID)
	if err != nil {
		return err
	}
	h.deletePreviousAvatar(r.Context(), previous)
	return h.writeBannerUser(w, r, user.ID)
}

func (h *Handler) writeBannerUser(w http.ResponseWriter, r *http.Request, id uuid.UUID) error {
	updated, err := auth.GetUser(r.Context(), h.DB, id)
	if err != nil {
		return err
	}
	if err := auth.LoadBanner(r.Context(), h.DB, updated); err != nil {
		return err
	}
	httpx.WriteJSON(w, http.StatusOK, updated)
	return nil
}

// readBanner reads, sniffs and redraws the 'banner' image of a multipart
// request. The result is what is stored; the upload itself never is.
func readBanner(r *http.Request) (*imgproc.Banner, error) {
	in, err := readFile(r, readFileOptions{field: "banner", maxBytes: MaxBannerBytes, allow: avatarMIME, imageOnly: true})
	if err != nil {
		return nil, err
	}
	defer in.file.Close()
	data, err := io.ReadAll(io.LimitReader(in.file, MaxBannerBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read banner: %w", err)
	}
	banner, err := imgproc.ProcessBanner(data)
	if errors.Is(err, imgproc.ErrImage) {
		return nil, httpx.ErrInvalidInput(err.Error())
	}
	if err != nil {
		return nil, err
	}
	if len(banner.Data) > MaxBannerBytes {
		return nil, httpx.ErrInvalidInput(fmt.Sprintf("the banner is larger than %d MB after processing", MaxBannerBytes>>20))
	}
	return banner, nil
}

// replaceBanner stores banner as userID's banner and deletes the previous one.
func (h *Handler) replaceBanner(ctx context.Context, userID uuid.UUID, banner *imgproc.Banner) error {
	mediaID := uuid.New()
	key := storageKey("banners", mediaID, banner.ContentType)
	if err := h.Store.Upload(ctx, key, bytes.NewReader(banner.Data), banner.ContentType, int64(len(banner.Data))); err != nil {
		return fmt.Errorf("store banner: %w", err)
	}
	in := &incomingFile{filename: "banner" + extensionFor(banner.ContentType), mime: banner.ContentType, size: int64(len(banner.Data))}
	previous, err := h.recordProfileImageTx(ctx, "banner_s3_key", userID, mediaID, key, in)
	if err != nil {
		_ = h.Store.Delete(context.WithoutCancel(ctx), key)
		return fmt.Errorf("record banner: %w", err)
	}
	h.deletePreviousAvatar(ctx, previous)
	return nil
}

// clearBanner removes the banner reference and returns the media ID it held.
func (h *Handler) clearBanner(ctx context.Context, userID uuid.UUID) (*string, error) {
	var previous *string
	err := pgx.BeginFunc(ctx, h.DB, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT banner_s3_key FROM users WHERE id = $1 FOR UPDATE`, userID).Scan(&previous); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE users SET banner_s3_key = NULL, updated_at = NOW() WHERE id = $1`, userID)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("clear banner: %w", err)
	}
	return previous, nil
}
