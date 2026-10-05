package linkpreview

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
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
// be fetched (the client then simply shows the plain link).
func (h *Handler) preview(w http.ResponseWriter, r *http.Request) error {
	p, err := h.Fetcher.Preview(r.Context(), r.URL.Query().Get("url"))
	if err != nil {
		w.WriteHeader(http.StatusNoContent)
		return nil
	}
	w.Header().Set("Cache-Control", "private, max-age=3600")
	httpx.WriteJSON(w, http.StatusOK, p)
	return nil
}

func (h *Handler) image(w http.ResponseWriter, r *http.Request) error {
	body, mime, err := h.Fetcher.Image(r.Context(), r.URL.Query().Get("url"))
	if errors.Is(err, ErrBlocked) {
		return httpx.ErrInvalidInput("not a public image address")
	}
	if err != nil {
		return httpx.ErrNotFound("image not available")
	}
	hdr := w.Header()
	hdr.Set("Content-Type", mime)
	hdr.Set("Content-Length", strconv.Itoa(len(body)))
	hdr.Set("X-Content-Type-Options", "nosniff")
	hdr.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	hdr.Set("Cache-Control", "private, max-age=86400")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
	return nil
}
