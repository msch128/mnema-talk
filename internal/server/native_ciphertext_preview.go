package server

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/chat"
	"github.com/msch128/mnema-talk/internal/httpx"
)

const nativeCiphertextPath = NativePreviewPrefix + "/channels/{channelID}/ciphertext-events"

type nativeCiphertextPage struct {
	Events    []chat.NativeCiphertextWire `json:"events"`
	NextAfter int64                       `json:"next_after"`
}
type NativeCiphertextPreviewRouter struct {
	*NativePreviewRouter
	Store *chat.NativeCiphertextStore
}
type nativeCiphertextPaging struct {
	After int64
	Limit int
}
type nativeCiphertextPagingKey struct{}

// NewNativeCiphertextPreviewRouter is an explicit blind-relay research fixture.
// Its current native principal authorizes transport/channel visibility only;
// no device/group/sender trust is minted here. All existing preview constructors
// and production routes stay untouched and advertise content unavailable.
func NewNativeCiphertextPreviewRouter(deps Deps, options NativePreviewOptions) (*NativeCiphertextPreviewRouter, error) {
	if deps.Config == nil {
		return nil, errors.New("invalid native relay dependencies")
	}
	trusted, err := httpx.ParseCIDRs(deps.Config.TrustedProxies)
	if err != nil {
		return nil, errors.New("invalid native relay trust")
	}
	preview, err := NewNativePublicPreviewRouter(deps, options)
	if err != nil {
		return nil, err
	}
	store, err := chat.NewNativeCiphertextStore(preview.Sessions)
	if err != nil {
		preview.Close()
		return nil, errors.New("native relay store unavailable")
	}
	result := &NativeCiphertextPreviewRouter{NativePreviewRouter: preview, Store: store}
	routes := chi.NewRouter()
	// Bound forged-bearer DB lookups before authentication as well as admitted
	// account work afterwards. Both use the existing trusted client-IP policy.
	perRelayIP := httpx.NewRateLimiter(120, time.Minute)
	routes.Use(httpx.RequestID, httpx.ClientIPMiddleware(trusted), httpx.Logger, httpx.Recover(deps.Config.IsProduction()), httpx.SecurityHeaders(trusted, deps.Config.IsProduction()), result.relayBoundary, perRelayIP.PerIP, preview.Native.RequireUser, preview.instanceContext, preview.preview.perUser.By(auth.UserKey))
	perRelayUser := httpx.NewRateLimiter(60, time.Minute)
	routes.Use(perRelayUser.By(auth.UserKey))
	routes.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteError(w, httpx.NewAPIError(http.StatusMethodNotAllowed, httpx.CodeInvalidInput, "method not allowed"))
	})
	routes.Post(nativeCiphertextPath, httpx.Handle(result.publishCiphertext))
	routes.Get(nativeCiphertextPath, httpx.Handle(result.listCiphertext))
	base := preview.Router.Handler
	preview.Router.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if nativeCiphertextRoute(r.URL.Path) {
			routes.ServeHTTP(w, r)
			return
		}
		base.ServeHTTP(w, r)
	})
	return result, nil
}
func nativeCiphertextRoute(path string) bool {
	const prefix = NativePreviewPrefix + "/channels/"
	return strings.HasPrefix(path, prefix) && strings.HasSuffix(path, "/ciphertext-events") && len(strings.Split(strings.Trim(path, "/"), "/")) == 6
}
func (r *NativeCiphertextPreviewRouter) relayBoundary(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		if nativePreviewHeaderPresent(request, "Origin") || nativePreviewHeaderPresent(request, "Cookie") || request.URL.User != nil || request.URL.Fragment != "" || request.URL.RawPath != "" {
			httpx.WriteError(w, httpx.ErrForbidden("native relay request rejected"))
			return
		}
		if r.options.Compatibility == NativeUnsupported {
			httpx.WriteError(w, httpx.ErrUnavailable("native API version unsupported"))
			return
		}
		paging, err := parseNativeCiphertextQuery(request.Method, request.URL)
		if err != nil {
			httpx.WriteError(w, err)
			return
		}
		// The reviewed auth boundary accepts no queries. Only validated fixed integer
		// paging is transferred to typed context; no untrusted query reaches auth and
		// no arbitrary target/header/credential is reconstructed.
		target := *request.URL
		target.RawQuery = ""
		target.ForceQuery = false
		forwarded := request.Clone(context.WithValue(request.Context(), nativeCiphertextPagingKey{}, paging))
		forwarded.URL = &target
		next.ServeHTTP(w, forwarded)
	})
}
func (r *NativeCiphertextPreviewRouter) publishCiphertext(w http.ResponseWriter, request *http.Request) error {
	principal, ok := auth.NativePrincipalFrom(request.Context())
	if !ok {
		return httpx.ErrUnauthorized("authentication required")
	}
	channel, err := nativeCiphertextChannel(request)
	if err != nil {
		return err
	}
	input, err := decodeNativeCiphertext(w, request, channel)
	if err != nil {
		return err
	}
	record, created, err := r.Store.Publish(request.Context(), principal, input)
	if err != nil {
		return nativeCiphertextError(err)
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	// Explicit wire copies are serialized only here. The record is opaque and
	// redacted in diagnostics, and no ciphertext is broadcast to metadata clients.
	httpx.WriteJSON(w, status, record.Wire())
	return nil
}
func (r *NativeCiphertextPreviewRouter) listCiphertext(w http.ResponseWriter, request *http.Request) error {
	principal, ok := auth.NativePrincipalFrom(request.Context())
	if !ok {
		return httpx.ErrUnauthorized("authentication required")
	}
	if request.ContentLength != 0 || len(request.TransferEncoding) != 0 || (request.Body != nil && request.Body != http.NoBody) {
		return httpx.ErrInvalidInput("native relay read body must be empty")
	}
	channel, err := nativeCiphertextChannel(request)
	if err != nil {
		return err
	}
	paging, ok := request.Context().Value(nativeCiphertextPagingKey{}).(nativeCiphertextPaging)
	if !ok {
		return httpx.ErrInvalidInput("invalid native relay pagination")
	}
	records, err := r.Store.List(request.Context(), principal, channel, paging.After, paging.Limit)
	if err != nil {
		return nativeCiphertextError(err)
	}
	page := nativeCiphertextPage{Events: make([]chat.NativeCiphertextWire, 0, len(records)), NextAfter: paging.After}
	for _, record := range records {
		wire := record.Wire()
		page.Events = append(page.Events, wire)
		page.NextAfter = wire.Number
	}
	httpx.WriteJSON(w, http.StatusOK, page)
	return nil
}
func nativeCiphertextError(err error) error {
	switch {
	case errors.Is(err, auth.ErrNativeUnauthorized):
		return httpx.ErrUnauthorized("authentication required")
	case errors.Is(err, chat.ErrNativeContentInput):
		return httpx.ErrInvalidInput("invalid native ciphertext request")
	case errors.Is(err, chat.ErrNativeContentChannel):
		return httpx.ErrNotFound("native ciphertext channel unavailable")
	case errors.Is(err, chat.ErrNativeContentConflict):
		return httpx.ErrConflict("native event identity already used")
	default:
		return httpx.ErrUnavailable("native ciphertext request unavailable")
	}
}
func parseNativeCiphertextQuery(method string, target *url.URL) (nativeCiphertextPaging, error) {
	paging := nativeCiphertextPaging{Limit: 25}
	invalid := func() (nativeCiphertextPaging, error) {
		return paging, httpx.ErrInvalidInput("invalid native relay pagination")
	}
	if target.ForceQuery && target.RawQuery == "" {
		return invalid()
	}
	if method != http.MethodGet {
		if target.RawQuery != "" {
			return invalid()
		}
		return paging, nil
	}
	if target.RawQuery == "" {
		return paging, nil
	}
	values, err := url.ParseQuery(target.RawQuery)
	if err != nil {
		return invalid()
	}
	if len(values) > 2 {
		return invalid()
	}
	for key, value := range values {
		if len(value) != 1 {
			return invalid()
		}
		switch key {
		case "after":
			n, ok := nativeCiphertextDecimal(value[0], 0, 9223372036854775807)
			if !ok {
				return invalid()
			}
			paging.After = n
		case "limit":
			n, ok := nativeCiphertextDecimal(value[0], 1, 100)
			if !ok {
				return invalid()
			}
			paging.Limit = int(n)
		default:
			return invalid()
		}
	}
	// Requiring canonical encoding also denies empty separators, encoded field
	// names/numbers and equivalent spellings before the native query is stripped.
	if values.Encode() != target.RawQuery {
		return invalid()
	}
	return paging, nil
}
