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
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/imgproc"
)

// profileImage is one sort of picture a member has: the users column that
// points at it, where its files live and how an upload is redrawn. The
// uploaded file is never stored, only the redrawn one (no EXIF, no trailing
// bytes).
type profileImage struct {
	field    string // multipart field
	column   string // users column holding the media ID (constant, never user input)
	prefix   string // storage key prefix
	maxBytes int64
	process  func([]byte) (*imgproc.Picture, error)
	// stamp is appended to the UPDATE of users when the picture is set.
	stamp string
}

var (
	avatarImage = profileImage{
		field: "avatar", column: "avatar_s3_key", prefix: "avatars", maxBytes: MaxAvatarBytes,
		process: imgproc.ProcessAvatar, stamp: ", avatar_reencoded_at = NOW()",
	}
	bannerImage = profileImage{
		field: "banner", column: "banner_s3_key", prefix: "banners", maxBytes: MaxBannerBytes,
		process: imgproc.ProcessBanner,
	}
)

// read reads, sniffs and redraws the picture of a multipart request.
func (p profileImage) read(r *http.Request) (*imgproc.Picture, error) {
	in, err := readFile(r, readFileOptions{field: p.field, maxBytes: p.maxBytes, allow: avatarMIME, imageOnly: true})
	if err != nil {
		return nil, err
	}
	defer in.file.Close()
	data, err := io.ReadAll(io.LimitReader(in.file, p.maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", p.field, err)
	}
	pic, err := p.process(data)
	if errors.Is(err, imgproc.ErrImage) {
		return nil, httpx.ErrInvalidInput(err.Error())
	}
	if err != nil {
		return nil, err
	}
	if int64(len(pic.Data)) > p.maxBytes {
		return nil, httpx.ErrInvalidInput(fmt.Sprintf("the %s is larger than %d MB after processing", p.field, p.maxBytes>>20))
	}
	return pic, nil
}

// set stores pic as userID's picture and deletes the previous one.
func (h *Handler) setProfileImage(ctx context.Context, p profileImage, userID uuid.UUID, pic *imgproc.Picture) error {
	mediaID := uuid.New()
	key := storageKey(p.prefix, mediaID, pic.ContentType)
	if err := h.Store.Upload(ctx, key, bytes.NewReader(pic.Data), pic.ContentType, int64(len(pic.Data))); err != nil {
		return fmt.Errorf("store %s: %w", p.field, err)
	}
	previous, err := h.recordProfileImageTx(ctx, p, userID, mediaID, key, pic, nil)
	if err != nil {
		_ = h.Store.Delete(context.WithoutCancel(ctx), key)
		return fmt.Errorf("record %s: %w", p.field, err)
	}
	h.deletePreviousAvatar(ctx, previous)
	return nil
}

// recordProfileImageTx writes the media row and points the user at it,
// returning the media ID it replaced. With expect set, it only does so while
// the user still has that picture (the re-encode job); otherwise it returns
// errPictureChanged.
func (h *Handler) recordProfileImageTx(ctx context.Context, p profileImage, userID, mediaID uuid.UUID, key string, pic *imgproc.Picture, expect *string) (*string, error) {
	var previous *string
	err := pgx.BeginFunc(ctx, h.DB, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT `+p.column+` FROM users WHERE id = $1 FOR UPDATE`, userID).Scan(&previous); err != nil {
			return err
		}
		if expect != nil && (previous == nil || *previous != *expect) {
			return errPictureChanged
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO media (id, uploader_id, s3_bucket, s3_key, original_filename, mime_type, size_bytes)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			mediaID, userID, h.Bucket, key, p.field+extensionFor(pic.ContentType), pic.ContentType, len(pic.Data)); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE users SET `+p.column+` = $1, updated_at = NOW()`+p.stamp+` WHERE id = $2`, mediaID.String(), userID)
		return err
	})
	return previous, err
}

var errPictureChanged = errors.New("the picture changed meanwhile")

// clear removes the picture reference and returns the media ID it held.
func (h *Handler) clearProfileImage(ctx context.Context, p profileImage, userID uuid.UUID) (*string, error) {
	var previous *string
	err := pgx.BeginFunc(ctx, h.DB, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT `+p.column+` FROM users WHERE id = $1 FOR UPDATE`, userID).Scan(&previous); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE users SET `+p.column+` = NULL, updated_at = NOW() WHERE id = $1`, userID)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("clear %s: %w", p.field, err)
	}
	return previous, nil
}
