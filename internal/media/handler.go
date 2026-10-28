package media

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/msch128/mnema-talk/internal/audit"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/events"
	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/perm"
)

const (
	MaxAvatarBytes  = 5 << 20
	maxFilenameLen  = 255
	multipartMemory = 8 << 20 // avatar forms stay in memory (body capped below this)
)

// Handler serves uploads, media downloads and the admin storage dashboard.
type Handler struct {
	DB             *db.Pool
	Store          Store
	Bucket         string
	Events         events.Publisher
	MaxUploadBytes int64
	// RetentionDays > 0 enables automatic pruning. Default 0: nothing is ever
	// deleted automatically.
	RetentionDays int
	// Online resolves @here in an upload's caption; nil means nobody is online.
	Online chat.OnlineSource
	// Perms decides who may upload to and download from which channel; nil
	// imposes no channel limits (tests; the router always sets it).
	Perms *perm.Service
	// Audit records removals the server does on its own (avatars it cannot
	// redraw); nil records nothing.
	Audit *audit.Log
	// Quota bounds the storage uploads may use; the zero value imposes none.
	Quota Quota

	reserved reservations
}

// UploadBodyLimit is the transport ceiling for upload routes: the file cap
// plus headroom for multipart framing and form fields.
func (h *Handler) UploadBodyLimit() int64 { return bodyLimit(h.MaxUploadBytes) }

func bodyLimit(fileLimit int64) int64 { return fileLimit + 1<<20 }

type limitKey struct{}

// WithUploadLimit returns ctx carrying the file size limit of one request.
func WithUploadLimit(ctx context.Context, bytes int64) context.Context {
	return context.WithValue(ctx, limitKey{}, bytes)
}

// limit is the file size limit of the request behind ctx, never above the
// operator's ceiling (MaxUploadBytes).
func (h *Handler) limit(ctx context.Context) int64 {
	if v, ok := ctx.Value(limitKey{}).(int64); ok && v < h.MaxUploadBytes {
		return v
	}
	return h.MaxUploadBytes
}

// memberLimit resolves what the signed-in member may upload: the Owner the
// ceiling, others the highest role override or the global limit.
func (h *Handler) memberLimit(ctx context.Context, sub perm.Subject) (int64, error) {
	if h.Perms == nil {
		return h.MaxUploadBytes, nil
	}
	v, err := h.Perms.UploadLimitBytes(ctx, sub)
	if err != nil {
		return 0, err
	}
	return min(v, h.MaxUploadBytes), nil
}

// requireStore answers 503 on routes that need object storage when none is
// configured, so the handlers behind it can use h.Store unconditionally.
func (h *Handler) requireStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.Store == nil {
			httpx.WriteError(w, httpx.ErrUnavailable("file storage is not configured"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// MountUploads registers the multipart routes; the caller wraps them in a
// larger MaxBody than the JSON default.
func (h *Handler) MountUploads(r chi.Router) {
	r = r.With(h.requireStore)
	r.Post("/channels/{channelID}/upload", httpx.Handle(h.upload))
	// An avatar is parsed in memory (at most 5 MB), never spilled to /tmp:
	// its body gets a tighter cap than file uploads.
	r.With(httpx.MaxBody(MaxAvatarBytes+1<<20)).Post("/users/me/avatar", httpx.Handle(h.uploadAvatar))
	r.With(httpx.MaxBody(MaxBannerBytes+1<<20)).Put("/users/me/banner", httpx.Handle(h.putBanner))
	r.Delete("/users/me/banner", httpx.Handle(h.deleteBanner))
	r.Delete("/users/me/avatar", httpx.Handle(h.deleteAvatar))
}

// Mount registers authenticated media serving.
func (h *Handler) Mount(r chi.Router) {
	r.With(h.requireStore).Get("/media/{id}", httpx.Handle(h.serve))
}

// MountAdmin registers the storage dashboard (Owner only). Pruning is
// opt-in: the manual prune needs an explicit day count unless
// MEDIA_RETENTION_DAYS is configured.
func (h *Handler) MountAdmin(r chi.Router) {
	r = r.With(auth.RequireOwner)
	r.Get("/media/stats", httpx.Handle(h.stats))
	r.Get("/media", httpx.Handle(h.list))
	r = r.With(h.requireStore)
	r.Post("/media/prune", httpx.Handle(h.prune))
	r.Get("/media/orphans", httpx.Handle(h.orphans))
	r.Post("/media/orphans/cleanup", httpx.Handle(h.cleanupOrphans))
	r.Delete("/media/{id}", httpx.Handle(h.delete))
}

// serve streams a media object to an authenticated member.
//
// @Summary Download a media object
// @Description Streams an attachment or avatar. Supports Range and conditional requests. Inline for safe types, attachment otherwise.
// @ID getMedia
// @Tags Media
// @Produce octet-stream,json
// @Security cookieAuth
// @Param id path string true "Resource ID." Format(uuid)
// @Success 200 {string} string "File content; Content-Type is the stored MIME type."
// @Success 206 {string} string "Partial content for a Range request."
// @Success 304 "Not modified (If-None-Match)."
// @Failure 400 {object} httpx.ErrorResponse "Invalid input (INVALID_INPUT): malformed JSON, unknown JSON fields, bad IDs or failed validation."
// @Failure 401 {object} httpx.ErrorResponse "No valid session (UNAUTHORIZED): missing, expired or revoked cookie, or the account was disabled."
// @Failure 404 {object} httpx.ErrorResponse "NOT_FOUND: the resource, or the route, does not exist."
// @Failure 410 {object} httpx.ErrorResponse "Media expired or was deleted. The error code is NOT_FOUND."
// @Failure 429 {object} httpx.ErrorResponse "RATE_LIMITED: too many requests."
// @Header 429 {integer} Retry-After "Seconds until the client may retry."
// @Failure 500 {object} httpx.ErrorResponse "INTERNAL_ERROR: sanitized server failure."
// @Failure 503 {object} httpx.ErrorResponse "UNAVAILABLE: a dependency (database, file storage) is not available."
// @Router /api/media/{id} [get]
func (h *Handler) serve(w http.ResponseWriter, r *http.Request) error {
	id, err := httpx.PathUUID(r, "id")
	if err != nil {
		return err
	}
	m, err := h.lookupServed(r, id)
	if err != nil {
		return err
	}

	etag := `"` + id.String() + `"`
	obj := &objectReader{ctx: r.Context(), store: h.Store, key: m.key, size: m.size}
	defer obj.Close()
	if err := prefetchObject(r, obj, etag, m); err != nil {
		return err
	}
	setServeHeaders(w.Header(), m, etag)
	// ServeContent answers Range (206, video seeking), If-None-Match and HEAD.
	http.ServeContent(w, r, "", time.Time{}, obj)
	return nil
}
