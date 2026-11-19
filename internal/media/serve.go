package media

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/mnema-xyz/talk/internal/auth"
	"github.com/mnema-xyz/talk/internal/httpx"
	"github.com/mnema-xyz/talk/internal/perm"
)

// servedMedia is the media row a download is answered from.
type servedMedia struct {
	id         uuid.UUID
	key        string
	mimeType   string
	filename   string
	size       int64
	channelID  *uuid.UUID
	wiki       bool
	messageID  *uuid.UUID
	staged     bool
	deleted    bool
	uploaderID uuid.UUID
}

// hideDenied turns a refusal into "not found", so a file the member may not see is not revealed.
func (h *Handler) hideDenied(err error) error {
	if _, denied := httpx.AsAPIError(err); denied && err != nil {
		return httpx.ErrNotFound("media not found")
	}
	return err
}

func (h *Handler) queryMedia(ctx context.Context, id uuid.UUID) (*servedMedia, error) {
	m := &servedMedia{id: id}
	err := h.DB.QueryRow(ctx, `
		SELECT m.s3_key, m.mime_type, m.original_filename, m.size_bytes, m.is_deleted, m.channel_id, m.staged, m.uploader_id,
			EXISTS (SELECT 1 FROM wiki_images w WHERE w.media_id = m.id),
			m.message_id
		FROM media m
		WHERE m.id = $1
		  -- the file of a message whose lifetime ended is gone with it, before the sweep
		  AND NOT EXISTS (SELECT 1 FROM messages x WHERE x.id = m.message_id AND x.expires_at <= now())`, id).
		Scan(&m.key, &m.mimeType, &m.filename, &m.size, &m.deleted, &m.channelID, &m.staged, &m.uploaderID, &m.wiki,
			&m.messageID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, httpx.ErrNotFound("media not found")
	}
	if err != nil {
		return nil, err
	}
	return m, nil
}

// authorizeServed enforces authorization rules for a media object. For bots,
// only attachments of messages in visible channels are allowed; any denial or
// unattached/deleted/staged media answers 404. For members, staged media belongs
// to its uploader, channel attachments require ViewChannel, wiki images require
// ViewWiki, and deleted media answers 410.
func (h *Handler) authorizeServed(ctx context.Context, sub perm.Subject, m *servedMedia, bot bool) error {
	if bot {
		if m.messageID == nil || m.channelID == nil || m.staged || m.deleted {
			return httpx.ErrNotFound("media not found")
		}
		return h.hideDenied(h.Perms.RequireChannel(ctx, sub, *m.channelID, perm.ViewChannel))
	}
	if m.staged && m.uploaderID != sub.ID {
		return httpx.ErrNotFound("media not found")
	}
	if m.channelID != nil {
		if err := h.hideDenied(h.Perms.RequireChannel(ctx, sub, *m.channelID, perm.ViewChannel)); err != nil {
			return err
		}
	}
	if m.wiki {
		if err := h.hideDenied(h.Perms.RequireGlobal(ctx, sub, perm.ViewWiki)); err != nil {
			return err
		}
	}
	if m.deleted {
		return httpx.NewAPIError(http.StatusGone, httpx.CodeNotFound, "media has expired or was deleted")
	}
	return nil
}

// lookupServed loads the media row and checks member authorization.
func (h *Handler) lookupServed(r *http.Request, id uuid.UUID) (*servedMedia, error) {
	m, err := h.queryMedia(r.Context(), id)
	if err != nil {
		return nil, err
	}
	user := auth.UserFrom(r.Context())
	if user == nil {
		return nil, httpx.ErrUnauthorized("401: Unauthorized")
	}
	if err := h.authorizeServed(r.Context(), user.Subject(), m, false); err != nil {
		return nil, err
	}
	return m, nil
}

// lookupBotServed loads the media row and checks bot authorization: the file
// must belong to a message in a channel or conversation visible to the bot.
func (h *Handler) lookupBotServed(r *http.Request, id uuid.UUID) (*servedMedia, error) {
	m, err := h.queryMedia(r.Context(), id)
	if err != nil {
		return nil, err
	}
	user := auth.UserFrom(r.Context())
	if user == nil {
		return nil, httpx.ErrUnauthorized("401: Unauthorized")
	}
	if err := h.authorizeServed(r.Context(), user.Subject(), m, true); err != nil {
		return nil, err
	}
	return m, nil
}

// prefetchObject opens the stored object before any header is written: once
// ServeContent has sent 200, a missing object could only end in a truncated
// body. A revalidation that ServeContent answers with 304 needs no fetch.
// For bots, a missing object answers 404; for members, 410 (StatusGone).
func prefetchObject(r *http.Request, obj *objectReader, etag string, m *servedMedia, bot bool) error {
	inm := r.Header.Get("If-None-Match")
	if strings.Contains(inm, etag) || strings.TrimSpace(inm) == "*" {
		return nil
	}
	if err := obj.open(rangeStart(r.Header.Get("Range"), m.size)); err != nil {
		if isMissingObject(err) {
			slog.Warn("media object missing from storage", "media_id", m.id, "key", m.key)
			if bot {
				return httpx.ErrNotFound("media not found")
			}
			return httpx.NewAPIError(http.StatusGone, httpx.CodeNotFound, "media file is missing from storage")
		}
		return fmt.Errorf("fetch media object: %w", err)
	}
	return nil
}

func setServeHeaders(hdr http.Header, m *servedMedia, etag string, forceAttachment bool) {
	disposition := "attachment"
	if !forceAttachment && inlineMIME[m.mimeType] {
		disposition = "inline"
	}
	hdr.Set("Content-Type", m.mimeType)
	hdr.Set("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": m.filename}))
	hdr.Set("X-Content-Type-Options", "nosniff")
	hdr.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	if m.channelID == nil && !m.wiki {
		hdr.Set("Cache-Control", "private, max-age=86400, immutable")
	} else {
		hdr.Set("Cache-Control", "private, no-cache")
	}
	hdr.Set("ETag", etag)
}

// serveMedia streams a media object to an authorized caller, setting CSP, nosniff,
// cache-control and ETag headers, and serving Range, HEAD and conditional (304) requests.
// When forceAttachment is true (bot downloads), Content-Disposition is forced to attachment.
func (h *Handler) serveMedia(w http.ResponseWriter, r *http.Request, m *servedMedia, forceAttachment bool) error {
	etag := `"` + m.id.String() + `"`
	obj := &objectReader{ctx: r.Context(), store: h.Store, key: m.key, size: m.size}
	defer obj.Close()
	if err := prefetchObject(r, obj, etag, m, forceAttachment); err != nil {
		return err
	}
	setServeHeaders(w.Header(), m, etag, forceAttachment)
	http.ServeContent(w, r, "", time.Time{}, obj)
	return nil
}
