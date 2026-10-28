package emoji

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/msch128/mnema-talk/internal/httpx"
)

// image handles GET /api/emoji/{id}/image.
//
// @Summary Custom emoji image
// @Description The redrawn PNG or GIF of the emoji. The `v` parameter is the content hash of the emoji's URL: the response is cacheable for a year.
// @ID getEmojiImage
// @Tags Emoji
// @Produce png,gif
// @Security cookieAuth
// @Param id path string true "Emoji ID." Format(uuid)
// @Param v query string false "Content hash from the emoji's URL."
// @Success 200 {string} binary "Image."
// @Success 304 "Not modified (If-None-Match)."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Failure 503 {object} httpx.ErrorResponse "UNAVAILABLE: a dependency (database, file storage) is not available."
// @Router /api/emoji/{id}/image [get]
func (h *Handler) image(w http.ResponseWriter, r *http.Request) error {
	return h.serve(false)(w, r)
}

// still handles GET /api/emoji/{id}/still.
//
// @Summary Custom emoji still frame
// @Description The first frame of an animated emoji as a PNG, for members who reduce motion. 404 when the emoji has no still frame (it is not animated). The `v` parameter is the content hash of the emoji's URL: the response is cacheable for a year.
// @ID getEmojiStill
// @Tags Emoji
// @Produce png,gif
// @Security cookieAuth
// @Param id path string true "Emoji ID." Format(uuid)
// @Param v query string false "Content hash from the emoji's URL."
// @Success 200 {string} binary "Image."
// @Success 304 "Not modified (If-None-Match)."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Failure 503 {object} httpx.ErrorResponse "UNAVAILABLE: a dependency (database, file storage) is not available."
// @Router /api/emoji/{id}/still [get]
func (h *Handler) still(w http.ResponseWriter, r *http.Request) error {
	return h.serve(true)(w, r)
}

// serve streams the image (or its still frame) of an emoji.
func (h *Handler) serve(still bool) httpx.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		if h.Store == nil {
			return httpx.ErrUnavailable("file storage is not configured")
		}
		id, err := httpx.PathUUID(r, "id")
		if err != nil {
			return err
		}
		e, err := h.get(r.Context(), id)
		if err != nil {
			return err
		}
		key, ctype := e.key, e.contentType
		if still {
			if e.stillKey == nil {
				return httpx.ErrNotFound("emoji has no still image")
			}
			key, ctype = *e.stillKey, TypePNG
		}
		return h.writeImage(w, r, key, ctype, e.hash, still)
	}
}

func (h *Handler) writeImage(w http.ResponseWriter, r *http.Request, key, ctype, hash string, still bool) error {
	etag := `"` + hash[:16]
	if still {
		etag += "-still"
	}
	etag += `"`
	hdr := w.Header()
	hdr.Set("ETag", etag)
	hdr.Set("Cache-Control", "private, max-age=31536000, immutable")
	if etagMatches(r.Header.Values("If-None-Match"), etag) {
		w.WriteHeader(http.StatusNotModified)
		return nil
	}
	body, err := h.Store.GetObjectFrom(r.Context(), key, 0)
	if err != nil {
		return fmt.Errorf("fetch emoji object: %w", err)
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, MaxFileBytes+1))
	if err != nil {
		return fmt.Errorf("read emoji object: %w", err)
	}
	hdr.Set("Content-Type", ctype)
	hdr.Set("Content-Length", fmt.Sprint(len(data)))
	hdr.Set("Content-Disposition", "inline")
	hdr.Set("X-Content-Type-Options", "nosniff")
	hdr.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	_, _ = w.Write(data)
	return nil
}

// etagMatches reports whether an If-None-Match header lists etag. It accepts
// several comma-separated validators, the weak form a proxy may rewrite them
// to (weak comparison, RFC 9110) and "*".
func etagMatches(headers []string, etag string) bool {
	for _, h := range headers {
		for _, v := range strings.Split(h, ",") {
			v = strings.TrimPrefix(strings.TrimSpace(v), "W/")
			if v == "*" || v == etag {
				return true
			}
		}
	}
	return false
}
