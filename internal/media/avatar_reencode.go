package media

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/imgproc"
)

// Avatars stored before 1.1 are the files members uploaded, EXIF block
// included. This job redraws each of them once (users.avatar_reencoded_at is
// the marker, set by the job and by every upload since). It repeats hourly so
// a storage outage at start only delays it; once nothing is pending, a run is
// one cheap query.
const (
	avatarReencodeFirstRun = 30 * time.Second
	avatarReencodeInterval = time.Hour
	avatarReencodeBatch    = 100
	// An old avatar was accepted up to MaxAvatarBytes; reading is bounded.
	avatarReadLimit = MaxAvatarBytes + 1
)

// StartAvatarReencode redraws every avatar that still is the uploaded file.
func StartAvatarReencode(ctx context.Context, p *db.Pool, store Store, bucket string) {
	if store == nil {
		return
	}
	h := &Handler{DB: p, Store: store, Bucket: bucket}
	go runPeriodically(ctx, avatarReencodeFirstRun, avatarReencodeInterval, func(ctx context.Context) {
		done, failed, err := h.ReencodeAvatars(ctx)
		switch {
		case err != nil:
			slog.Warn("avatar re-encode failed", "err", err)
		case done > 0 || failed > 0:
			slog.Info("avatars re-encoded without metadata", "done", done, "retry_later", failed)
		}
	})
}

type storedAvatar struct {
	userID  uuid.UUID
	mediaID string
	key     string
}

// ReencodeAvatars redraws the avatars not yet marked and returns how many it
// finished and how many it could not reach (storage trouble; the next run
// tries again). A picture that cannot be decoded is marked as done and kept:
// there is nothing to redraw it from.
func (h *Handler) ReencodeAvatars(ctx context.Context) (done, failed int, err error) {
	tried := []uuid.UUID{}
	for ctx.Err() == nil {
		batch, err := h.pendingAvatars(ctx, tried)
		if err != nil || len(batch) == 0 {
			return done, failed, err
		}
		for _, a := range batch {
			tried = append(tried, a.userID)
			if err := h.reencodeAvatar(ctx, a); err != nil {
				slog.Warn("could not re-encode an avatar", "user_id", a.userID, "err", err)
				failed++
				continue
			}
			done++
		}
	}
	return done, failed, ctx.Err()
}

func (h *Handler) pendingAvatars(ctx context.Context, tried []uuid.UUID) ([]storedAvatar, error) {
	rows, err := h.DB.Query(ctx, `
		SELECT u.id, m.id::text, m.s3_key
		FROM users u JOIN media m ON m.id::text = u.avatar_s3_key
		WHERE u.avatar_reencoded_at IS NULL AND NOT m.is_deleted AND u.id <> ALL($1)
		ORDER BY u.id LIMIT $2`, tried, avatarReencodeBatch)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (a storedAvatar, err error) {
		err = row.Scan(&a.userID, &a.mediaID, &a.key)
		return a, err
	})
}

func (h *Handler) reencodeAvatar(ctx context.Context, a storedAvatar) error {
	body, err := h.Store.GetObjectFrom(ctx, a.key, 0)
	if err != nil {
		return fmt.Errorf("read avatar: %w", err)
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, avatarReadLimit))
	if err != nil {
		return fmt.Errorf("read avatar: %w", err)
	}
	pic, err := imgproc.ProcessAvatar(data)
	if errors.Is(err, imgproc.ErrImage) {
		slog.Warn("an old avatar cannot be redrawn and is kept as it is", "user_id", a.userID, "err", err)
		_, err = h.DB.Exec(ctx, `UPDATE users SET avatar_reencoded_at = NOW() WHERE id = $1 AND avatar_s3_key = $2`, a.userID, a.mediaID)
		return err
	}
	if err != nil {
		return err
	}
	return h.swapAvatar(ctx, a, pic)
}

// swapAvatar writes the redrawn picture, points the account at it and only
// then deletes the old object.
func (h *Handler) swapAvatar(ctx context.Context, a storedAvatar, pic *imgproc.Picture) error {
	newID := uuid.New()
	newKey := storageKey(avatarImage.prefix, newID, pic.ContentType)
	if err := h.Store.Upload(ctx, newKey, bytes.NewReader(pic.Data), pic.ContentType, int64(len(pic.Data))); err != nil {
		return fmt.Errorf("store avatar: %w", err)
	}
	previous, err := h.recordProfileImageTx(ctx, avatarImage, a.userID, newID, newKey, pic, &a.mediaID)
	if errors.Is(err, errPictureChanged) {
		// The member set a new avatar meanwhile: that one is already redrawn.
		_ = h.Store.Delete(context.WithoutCancel(ctx), newKey)
		return nil
	}
	if err != nil {
		_ = h.Store.Delete(context.WithoutCancel(ctx), newKey)
		return fmt.Errorf("record avatar: %w", err)
	}
	h.deletePreviousAvatar(ctx, previous)
	return nil
}
