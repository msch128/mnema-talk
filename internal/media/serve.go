package media

import (
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

// servedMedia is the media row a download is answered from.
type servedMedia struct {
	id        uuid.UUID
	key       string
	mimeType  string
	filename  string
	size      int64
	channelID *uuid.UUID
}

// lookupServed loads the media row and checks that the caller may fetch it:
// an attachment belongs to its channel, so whoever cannot view the channel
// cannot fetch the file either (avatars have no channel). Deleted media is 410.
func (h *Handler) lookupServed(r *http.Request, id uuid.UUID) (*servedMedia, error) {
	m := &servedMedia{id: id}
	var deleted, staged bool
	var uploader uuid.UUID
	err := h.DB.QueryRow(r.Context(), `
		SELECT s3_key, mime_type, original_filename, size_bytes, is_deleted, channel_id, staged, uploader_id FROM media WHERE id = $1`, id).
		Scan(&m.key, &m.mimeType, &m.filename, &m.size, &deleted, &m.channelID, &staged, &uploader)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound("media not found")
	}
	if err != nil {
		return nil, err
	}
	// A file that is not sent yet belongs to its uploader alone.
	if staged && uploader != auth.UserFrom(r.Context()).ID {
		return nil, httpx.ErrNotFound("media not found")
	}
	if m.channelID != nil {
		if err := h.Perms.RequireChannel(r.Context(), auth.UserFrom(r.Context()).Subject(), *m.channelID, perm.ViewChannel); err != nil {
			if _, denied := httpx.AsAPIError(err); denied {
				return nil, httpx.ErrNotFound("media not found")
			}
			return nil, err
		}
	}
	if deleted {
		return nil, httpx.NewAPIError(http.StatusGone, httpx.CodeNotFound, "media has expired or was deleted")
	}
	return m, nil
}

// prefetchObject opens the stored object before any header is written: once
// ServeContent has sent 200, a missing object could only end in a truncated
// body. A revalidation that ServeContent answers with 304 needs no fetch.
func prefetchObject(r *http.Request, obj *objectReader, etag string, m *servedMedia) error {
	inm := r.Header.Get("If-None-Match")
	if strings.Contains(inm, etag) || strings.TrimSpace(inm) == "*" {
		return nil
	}
	if err := obj.open(rangeStart(r.Header.Get("Range"), m.size)); err != nil {
		if isMissingObject(err) {
			slog.Warn("media object missing from storage", "media_id", m.id, "key", m.key)
			return httpx.NewAPIError(http.StatusGone, httpx.CodeNotFound, "media file is missing from storage")
		}
		return fmt.Errorf("fetch media object: %w", err)
	}
	return nil
}

func setServeHeaders(hdr http.Header, m *servedMedia, etag string) {
	disposition := "attachment"
	if inlineMIME[m.mimeType] {
		disposition = "inline"
	}
	hdr.Set("Content-Type", m.mimeType)
	hdr.Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": m.filename}))
	hdr.Set("X-Content-Type-Options", "nosniff")
	hdr.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	// Attachments are revalidated on every use (the ETag answers 304 after the
	// permission and deletion checks), so a removed file or a revoked channel
	// stops working at once instead of living on in the browser cache.
	// Avatars belong to no channel and are replaced under a new ID, so they
	// may be cached; private keeps shared caches out in both cases.
	if m.channelID == nil {
		hdr.Set("Cache-Control", "private, max-age=86400, immutable")
	} else {
		hdr.Set("Cache-Control", "private, no-cache")
	}
	hdr.Set("ETag", etag)
}
