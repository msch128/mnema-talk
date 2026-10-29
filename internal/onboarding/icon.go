package onboarding

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/imgproc"
	"github.com/msch128/mnema-talk/internal/media"
	"github.com/msch128/mnema-talk/internal/perm"
)

// The community icon is the picture the Owner picks for the community, like a
// server icon elsewhere. It is public on purpose: the name it sits next to is
// public already (GET /api/health, the sign-in page, the link previews), so a
// client can show it before anyone signs in, and a messenger can use it in a
// link preview. Only the Owner and Administrators set it.
const (
	// MaxIconBytes is the size of an uploaded icon.
	MaxIconBytes = 5 << 20
	// IconBodyLimit is the transport ceiling of the upload route.
	IconBodyLimit = MaxIconBytes + 1<<20
	iconField     = "icon"
	// iconMaxAge is how long a client may reuse the icon without asking; a
	// request that names the current version (?v=) is immutable.
	iconMaxAge = 300
)

func iconKey(version string, size int) string {
	return fmt.Sprintf("community/icon/%s/%d.png", version, size)
}

func newIconVersion() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("icon version: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// mountPublicIcon registers the icon route that needs no session.
func (h *Handler) mountPublicIcon(r chi.Router) {
	r.With(h.iconPerIP.PerIP).Get("/community/icon", httpx.Handle(h.getIcon))
}

// MountAdminIconUploads registers the upload routes of the icon (outside the
// JSON body limit); Administrators only.
func (h *Handler) MountAdminIconUploads(r chi.Router) {
	admin := r.With(auth.RequirePerm(h.Perms, perm.Administrator))
	admin.Put("/community/icon", httpx.Handle(h.putIcon))
	admin.Delete("/community/icon", httpx.Handle(h.deleteIcon))
}

// getIcon handles GET /api/community/icon.
//
// @Summary Community icon
// @Description The icon the Owner set for the community, a square PNG. Public (no session): the community name next to it is public too, also on an invite-only server. `size` is 128 or 512 (default). 404 (NOT_FOUND) while no icon is set. The `community_icon` field of GET /api/health holds the current version; a request with `v` equal to it is cacheable for a year (immutable), any other is revalidated with ETag and Last-Modified after 5 minutes.
// @ID getCommunityIcon
// @Tags Onboarding
// @Produce png
// @Param size query int false "Edge in pixels." Enums(128, 512) default(512)
// @Param v query string false "Version from GET /api/health (community_icon)."
// @Success 200 {string} binary "PNG."
// @Success 304 "Not modified (If-None-Match, If-Modified-Since)."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): size is not 128 or 512."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: no icon is set."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Failure 503 {object} httpx.ErrorResponse "UNAVAILABLE: a dependency (database, file storage) is not available."
// @Router /api/community/icon [get]
func (h *Handler) getIcon(w http.ResponseWriter, r *http.Request) error {
	size, err := iconSize(r)
	if err != nil {
		return err
	}
	var version *string
	var updated *time.Time
	err = h.DB.QueryRow(r.Context(), `SELECT community_icon_version, community_icon_updated_at FROM instance_settings`).Scan(&version, &updated)
	if err != nil {
		return fmt.Errorf("read community icon: %w", err)
	}
	if version == nil || h.Store == nil {
		return httpx.ErrNotFound("no community icon is set")
	}
	modified := time.Time{}
	if updated != nil {
		modified = *updated
	}
	etag := setIconCacheHeaders(w, r, *version, size)
	// Answer a conditional request before touching object storage.
	if notModified(r, etag, modified) {
		w.WriteHeader(http.StatusNotModified)
		return nil
	}
	data, err := h.readIcon(r.Context(), *version, size)
	if err != nil {
		return err
	}
	hdr := w.Header()
	hdr.Set("Content-Type", "image/png")
	hdr.Set("X-Content-Type-Options", "nosniff")
	hdr.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	http.ServeContent(w, r, "", modified, bytes.NewReader(data))
	return nil
}

// iconSize reads the size parameter: 512 unless 128 is asked for.
func iconSize(r *http.Request) (int, error) {
	q := r.URL.Query().Get("size")
	if q == "" {
		return imgproc.IconLargeEdge, nil
	}
	n, err := strconv.Atoi(q)
	if err != nil || (n != imgproc.IconLargeEdge && n != imgproc.IconSmallEdge) {
		return 0, httpx.ErrInvalidInput("size must be 128 or 512")
	}
	return n, nil
}

// setIconCacheHeaders sets the validators and the lifetime and returns the
// ETag. A request that names the current version may keep the icon for good.
func setIconCacheHeaders(w http.ResponseWriter, r *http.Request, version string, size int) string {
	etag := `"` + version + "-" + strconv.Itoa(size) + `"`
	hdr := w.Header()
	hdr.Set("ETag", etag)
	if r.URL.Query().Get("v") == version {
		hdr.Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		hdr.Set("Cache-Control", "public, max-age="+strconv.Itoa(iconMaxAge)+", must-revalidate")
	}
	return etag
}

// readIcon reads one stored file of the icon.
func (h *Handler) readIcon(ctx context.Context, version string, size int) ([]byte, error) {
	body, err := h.Store.GetObjectFrom(ctx, iconKey(version, size), 0)
	if err != nil {
		if media.IsMissingObject(err) {
			return nil, httpx.ErrNotFound("no community icon is set")
		}
		return nil, fmt.Errorf("fetch community icon: %w", err)
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, MaxIconBytes))
	if err != nil {
		return nil, fmt.Errorf("read community icon: %w", err)
	}
	return data, nil
}

// notModified reports whether the validators of the request match the icon.
func notModified(r *http.Request, etag string, modified time.Time) bool {
	if inm := r.Header.Values("If-None-Match"); len(inm) > 0 {
		for _, h := range inm {
			for _, v := range splitETags(h) {
				if v == "*" || v == etag {
					return true
				}
			}
		}
		return false
	}
	if ims := r.Header.Get("If-Modified-Since"); ims != "" && !modified.IsZero() {
		if t, err := http.ParseTime(ims); err == nil {
			return !modified.Truncate(time.Second).After(t)
		}
	}
	return false
}

// splitETags splits an If-None-Match value into its validators, weak ones
// (W/) compared as strong.
func splitETags(header string) []string {
	var out []string
	for _, v := range bytes.Split([]byte(header), []byte(",")) {
		v = bytes.TrimPrefix(bytes.TrimSpace(v), []byte("W/"))
		out = append(out, string(v))
	}
	return out
}

// putIcon handles PUT /api/admin/community/icon.
//
// @Summary Set the community icon
// @Description Owner and Administrators only; audited. multipart/form-data with an image in field 'icon' (PNG, JPEG, GIF or WebP, at most 5 MB, at least 128x128 pixels; SVG and animations are rejected). The server never stores the upload: it decodes it, crops the centre square and writes new 512x512 and 128x128 PNG files, so metadata such as EXIF is gone. Replaces the previous icon. Broadcasts community_updated.
// @ID setCommunityIcon
// @Tags Onboarding
// @Accept mpfd
// @Produce json
// @Security cookieAuth
// @Param icon formData file true "Image file, at most 5 MB."
// @Success 200 {object} Settings "Settings, with the new community_icon version."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): no file, too small or too large in pixels, animated, or the picture cannot be read."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: only the Owner and members with the Administrator permission may change it, or cross-origin request rejected."
// @Failure 413 {object} httpx.ErrorResponse "PAYLOAD_TOO_LARGE: body or file exceeds the limit."
// @Failure 415 {object} httpx.ErrorResponse "UNSUPPORTED_MEDIA_TYPE: the file type is not allowed."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Failure 503 {object} httpx.ErrorResponse "UNAVAILABLE: a dependency (database, file storage) is not available."
// @Router /api/admin/community/icon [put]
func (h *Handler) putIcon(w http.ResponseWriter, r *http.Request) error {
	if h.Store == nil {
		return httpx.ErrUnavailable("file storage is not configured")
	}
	var icon *imgproc.Icon
	_, err := media.ReadPicture(r, media.PictureUpload{
		Fields:   []string{iconField},
		MaxBytes: MaxIconBytes,
		Process: func(data []byte) (*imgproc.Picture, error) {
			var err error
			icon, err = imgproc.ProcessIcon(data)
			if err != nil {
				return nil, err
			}
			return &imgproc.Picture{Data: icon.Large, ContentType: "image/png"}, nil
		},
	})
	if err != nil {
		return err
	}
	version, err := newIconVersion()
	if err != nil {
		return err
	}
	ctx := r.Context()
	if err := h.storeIcon(ctx, version, icon); err != nil {
		return err
	}
	previous, err := h.swapIcon(ctx, &version)
	if err != nil {
		h.deleteIconObjects(ctx, version)
		return err
	}
	if previous != "" {
		h.deleteIconObjects(ctx, previous)
	}
	return h.iconChanged(w, r, true)
}

// deleteIcon handles DELETE /api/admin/community/icon.
//
// @Summary Remove the community icon
// @Description Owner and Administrators only; audited. Deletes the icon and its files; the Mnema icon shows again. Succeeds when there is none. Broadcasts community_updated.
// @ID deleteCommunityIcon
// @Tags Onboarding
// @Produce json
// @Security cookieAuth
// @Success 200 {object} Settings "Settings, without community_icon."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 403 {object} httpx.ErrorResponse "FORBIDDEN: only the Owner and members with the Administrator permission may change it, or cross-origin request rejected."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/admin/community/icon [delete]
func (h *Handler) deleteIcon(w http.ResponseWriter, r *http.Request) error {
	previous, err := h.swapIcon(r.Context(), nil)
	if err != nil {
		return err
	}
	if previous != "" && h.Store != nil {
		h.deleteIconObjects(r.Context(), previous)
	}
	return h.iconChanged(w, r, previous != "")
}

func (h *Handler) storeIcon(ctx context.Context, version string, icon *imgproc.Icon) error {
	for size, data := range map[int][]byte{imgproc.IconLargeEdge: icon.Large, imgproc.IconSmallEdge: icon.Small} {
		if err := h.Store.Upload(ctx, iconKey(version, size), bytes.NewReader(data), "image/png", int64(len(data))); err != nil {
			h.deleteIconObjects(ctx, version)
			return fmt.Errorf("store community icon: %w", err)
		}
	}
	return nil
}

// swapIcon points the settings at version (nil: no icon) and returns the
// version it replaced, "" when there was none.
func (h *Handler) swapIcon(ctx context.Context, version *string) (string, error) {
	var previous *string
	err := pgx.BeginFunc(ctx, h.DB, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT community_icon_version FROM instance_settings FOR UPDATE`).Scan(&previous); err != nil {
			return err
		}
		if version == nil && previous == nil {
			return nil
		}
		var at *time.Time
		if version != nil {
			now := time.Now()
			at = &now
		}
		_, err := tx.Exec(ctx, `UPDATE instance_settings SET community_icon_version = $1, community_icon_updated_at = $2, updated_at = NOW()`, version, at)
		return err
	})
	if err != nil {
		return "", fmt.Errorf("write community icon: %w", err)
	}
	if previous == nil {
		return "", nil
	}
	return *previous, nil
}

// deleteIconObjects removes both files of a version. It runs to the end when
// the request goes away and only logs a failure: the settings are updated.
func (h *Handler) deleteIconObjects(ctx context.Context, version string) {
	keys := []string{iconKey(version, imgproc.IconLargeEdge), iconKey(version, imgproc.IconSmallEdge)}
	if err := h.Store.DeleteBatch(context.WithoutCancel(ctx), keys); err != nil {
		slog.WarnContext(ctx, "could not delete community icon files", "version", version, "err", err)
	}
}

// iconChanged tells the caches and, when something changed, the open clients,
// then answers with the settings.
func (h *Handler) iconChanged(w http.ResponseWriter, r *http.Request, changed bool) error {
	h.Names.Forget()
	s, err := scanSettings(h.DB.QueryRow(r.Context(), `SELECT `+settingsColumns+` FROM instance_settings`))
	if err != nil {
		return fmt.Errorf("read onboarding settings: %w", err)
	}
	if changed {
		h.broadcastCommunity(r.Context(), s)
	}
	httpx.WriteJSON(w, http.StatusOK, s)
	return nil
}

func (h *Handler) broadcastCommunity(ctx context.Context, s Settings) {
	if h.Events == nil {
		return
	}
	name := s.CommunityName
	icon := s.CommunityIcon
	if h.Names != nil {
		name = h.Names.Get(ctx)
	}
	h.Events.Broadcast(events.CommunityUpdated, map[string]string{"community_name": name, "community_icon": icon})
}
