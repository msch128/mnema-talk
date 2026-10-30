package linkpreview

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// Handler serves link cards and their proxied images to signed-in members.
type Handler struct {
	Fetcher *Fetcher
}

func (h *Handler) Mount(r chi.Router) {
	r.Get("/link-preview", httpx.Handle(h.preview))
	r.Get("/link-preview/image", httpx.Handle(h.image))
}

// preview answers 200 with a card, or 204 when the page has none or may not
// be fetched (the client then simply shows the plain link). Fetch capacity
// exhaustion answers 429 so clients can retry instead of caching no preview.
//
// @Summary Link card for a URL
// @Description Only mounted when LINK_PREVIEWS is enabled. Reads Open Graph and Twitter card tags, the theme colour and oEmbed; YouTube and Vimeo links use the provider's oEmbed, Steam store links add price and recommendations. `embed` is set for YouTube and Vimeo videos only, with a fixed player address. Answers 204 when the page has no card or may not be fetched. Extra rate limit (120 per minute).
// @ID getLinkPreview
// @Tags Media
// @Produce json
// @Security cookieAuth
// @Param url query string true "Absolute URL of the page (or image) to fetch." Format(uri)
// @Success 200 {object} Preview "Link card."
// @Success 204 "No preview available."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests or outbound fetch capacity exhausted."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Router /api/link-preview [get]
func (h *Handler) preview(w http.ResponseWriter, r *http.Request) error {
	p, err := h.Fetcher.Preview(requester(r), r.URL.Query().Get("url"))
	if errors.Is(err, errBusy) {
		httpx.WriteRateLimited(w, 0)
		return nil
	}
	if err != nil {
		w.WriteHeader(http.StatusNoContent)
		return nil
	}
	w.Header().Set("Cache-Control", "private, max-age=3600")
	httpx.WriteJSON(w, http.StatusOK, p)
	return nil
}

// image handles GET /api/link-preview/image.
//
// @Summary Proxied preview image
// @Description Only mounted when LINK_PREVIEWS is enabled. Fetches a public image through the server because the CSP only allows same-origin images.
// @ID getLinkPreviewImage
// @Tags Media
// @Produce image/*,json
// @Security cookieAuth
// @Param url query string true "Absolute URL of the page (or image) to fetch." Format(uri)
// @Success 200 {string} string "Image bytes."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the address has no usable image, or the route does not exist."
// @Failure 410 {object} httpx.ErrorResponse "The image was taken down (the address answered 404 or 410). The error code is NOT_FOUND."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests or outbound fetch capacity exhausted."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Failure 503 {object} httpx.ErrorResponse "UNAVAILABLE: the image could not be fetched right now; try again."
// @Router /api/link-preview/image [get]
func (h *Handler) image(w http.ResponseWriter, r *http.Request) error {
	body, mime, err := h.Fetcher.Image(requester(r), r.URL.Query().Get("url"))
	if errors.Is(err, errBusy) {
		httpx.WriteRateLimited(w, 0)
		return nil
	}
	if errors.Is(err, ErrBlocked) {
		return httpx.ErrInvalidInput("not a public image address")
	}
	var status statusError
	switch {
	case err == nil:
	case errors.As(err, &status) && status.gone():
		return httpx.NewAPIError(http.StatusGone, httpx.CodeNotFound, "image is gone")
	case errors.Is(err, ErrNoPreview) || (errors.As(err, &status) && status.refused()):
		return httpx.ErrNotFound("image not available")
	default:
		// Timeouts, a failing host: the image may well be there next time.
		return httpx.ErrUnavailable("image could not be fetched")
	}
	hdr := w.Header()
	hdr.Set("Content-Type", mime)
	hdr.Set("Content-Length", strconv.Itoa(len(body)))
	hdr.Set("X-Content-Type-Options", "nosniff")
	hdr.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	hdr.Set("Cache-Control", "private, max-age=86400")
	w.WriteHeader(http.StatusOK)
	// The headers are out: a failed write (client gone) cannot become an error response.
	if _, err := w.Write(body); err != nil {
		slog.Debug("link preview image write", "err", err)
	}
	return nil
}

// requester tags the request context with the signed-in member, so one
// member's fetches cannot take every server-wide fetch slot.
func requester(r *http.Request) context.Context {
	if u := auth.UserFrom(r.Context()); u != nil {
		return WithRequester(r.Context(), u.ID.String())
	}
	return r.Context()
}
