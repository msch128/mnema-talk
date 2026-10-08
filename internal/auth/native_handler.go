package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/httpx"
)

const nativeAuthMaxBody int64 = 16 << 10

// NativeHandler is intentionally not wired into the server router. These
// endpoints issue transport sessions, not E2EE device or content authorization.
// Remote API access belongs to the native broker; tokens never belong in a
// renderer, browser cookie, URL, generic DTO or diagnostics.
type NativeHandler struct {
	accounts     *Handler
	sessions     *NativeSessions
	refreshPerIP *httpx.RateLimiter
}

func NewNativeHandler(accounts *Handler, sessions *NativeSessions) (*NativeHandler, error) {
	// Credential proof and grant persistence must belong to the same instance.
	if accounts == nil || accounts.Sessions == nil || sessions == nil ||
		accounts.Sessions.DB == nil || accounts.Sessions.DB != sessions.pool ||
		accounts.loginPerIP == nil || accounts.loginFailures == nil || accounts.accountFailures == nil {
		return nil, errors.New("invalid native authentication dependencies")
	}
	return &NativeHandler{accounts: accounts, sessions: sessions,
		refreshPerIP: httpx.NewRateLimiter(120, time.Minute)}, nil
}

// Handler returns an isolated router with relative /auth/* endpoints. Calling
// it does not expose routes in production. Future mounting requires separate
// transport/compatibility review and the native-client security gates.
func (h *NativeHandler) Handler() http.Handler {
	r := chi.NewRouter()
	r.Use(nativeRequestBoundary)
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteError(w, httpx.ErrNotFound("route not found"))
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteError(w, httpx.NewAPIError(http.StatusMethodNotAllowed, httpx.CodeInvalidInput, "method not allowed"))
	})
	// Exactly the same IP budget as browser login, counted once per request.
	r.With(h.accounts.loginPerIP.PerIP).Post("/auth/login", httpx.Handle(h.login))
	// A separate budget avoids legitimate refreshes exhausting login attempts.
	// An IP-exhausted replay is not processed; admitted replay handling in the
	// service still precedes the per-family refresh-success throttle.
	r.With(h.refreshPerIP.PerIP).Post("/auth/refresh", httpx.Handle(h.refresh))
	r.Group(func(authed chi.Router) {
		authed.Use(h.RequireUser)
		authed.Get("/auth/me", httpx.Handle(h.me))
		authed.Post("/auth/logout", httpx.Handle(h.logout))
	})
	return r
}

func nativeHeaderValues(r *http.Request, name string) []string {
	var values []string
	for key, entries := range r.Header {
		if strings.EqualFold(key, name) {
			values = append(values, entries...)
		}
	}
	return values
}

func nativeHeaderPresent(r *http.Request, name string) bool {
	for key := range r.Header {
		if strings.EqualFold(key, name) {
			return true
		}
	}
	return false
}

func nativeRequestBoundary(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if nativeHeaderPresent(r, "Origin") || nativeHeaderPresent(r, "Cookie") {
			httpx.WriteError(w, httpx.ErrForbidden("native transport request rejected"))
			return
		}
		if r.URL.RawQuery != "" || r.URL.User != nil || r.URL.Fragment != "" {
			httpx.WriteError(w, httpx.ErrInvalidInput("invalid native request target"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func nativeBearer(r *http.Request) (string, error) {
	values := nativeHeaderValues(r, "Authorization")
	if len(values) != 1 || len(values[0]) != 50 || !strings.HasPrefix(values[0], "Bearer ") {
		return "", ErrNativeUnauthorized
	}
	token := values[0][7:]
	if _, err := parseNativeSecret(token); err != nil {
		return "", err
	}
	return token, nil
}

func noNativeAuthorization(r *http.Request) error {
	if nativeHeaderPresent(r, "Authorization") {
		return httpx.ErrInvalidInput("authorization not accepted here")
	}
	return nil
}

// Parse a small object of exactly known string fields, rejecting duplicate keys,
// null, non-object roots and trailing values without echoing decoder errors.
func decodeNativeStrings(w http.ResponseWriter, r *http.Request, fields ...string) (map[string]string, error) {
	contentTypes := nativeHeaderValues(r, "Content-Type")
	if len(contentTypes) != 1 {
		return nil, httpx.ErrUnsupportedMediaType("native authentication requires JSON")
	}
	mediaType, _, typeErr := mime.ParseMediaType(contentTypes[0])
	if typeErr != nil || mediaType != "application/json" {
		return nil, httpx.ErrUnsupportedMediaType("native authentication requires JSON")
	}
	if r.Body == nil {
		return nil, httpx.ErrInvalidInput("invalid native JSON body")
	}
	if r.ContentLength > nativeAuthMaxBody {
		return nil, httpx.ErrPayloadTooLarge("request body too large")
	}
	r.Body = http.MaxBytesReader(w, r.Body, nativeAuthMaxBody)
	decoder := json.NewDecoder(r.Body)
	invalid := func(err error) error {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			return httpx.ErrPayloadTooLarge("request body too large")
		}
		return httpx.ErrInvalidInput("invalid native JSON body")
	}
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, invalid(err)
	}
	values := make(map[string]string, len(fields))
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, invalid(err)
		}
		key, ok := token.(string)
		if !ok {
			return nil, invalid(nil)
		}
		known := false
		for _, allowed := range fields {
			if key == allowed {
				known = true
				break
			}
		}
		if !known {
			return nil, invalid(nil)
		}
		if _, duplicate := values[key]; duplicate {
			return nil, invalid(nil)
		}
		token, err = decoder.Token()
		if err != nil {
			return nil, invalid(err)
		}
		value, ok := token.(string)
		if !ok {
			return nil, invalid(nil)
		}
		values[key] = value
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') {
		return nil, invalid(err)
	}
	if _, err = decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, invalid(err)
	}
	if len(values) != len(fields) {
		return nil, invalid(nil)
	}
	return values, nil
}

func emptyNativeBody(r *http.Request) error {
	if r.Body == nil {
		return nil
	}
	var one [1]byte
	n, err := io.ReadFull(r.Body, one[:])
	if n == 0 && errors.Is(err, io.EOF) {
		return nil
	}
	return httpx.ErrInvalidInput("native request body must be empty")
}

// This response is the sole explicit token wire conversion. Its nativeSecret
// wrapper allows JSON here while keeping all formatting and log values redacted.
type nativeWireSecret struct{ value nativeSecret }

func (nativeWireSecret) Format(state fmt.State, _ rune) {
	_, _ = io.WriteString(state, "[native wire secret redacted]")
}

func (nativeWireSecret) String() string   { return "[native wire secret redacted]" }
func (nativeWireSecret) GoString() string { return "[native wire secret redacted]" }
func (nativeWireSecret) LogValue() slog.Value {
	return slog.StringValue("[native wire secret redacted]")
}
func (s nativeWireSecret) MarshalJSON() ([]byte, error) { return json.Marshal(s.value.wire()) }

type nativeGrantResponse struct {
	User             User             `json:"user"`
	FamilyID         uuid.UUID        `json:"family_id"`
	ClientInstanceID uuid.UUID        `json:"client_instance_id"`
	RefreshSequence  uint64           `json:"refresh_sequence"`
	AccessToken      nativeWireSecret `json:"access_token"`
	RefreshToken     nativeWireSecret `json:"refresh_token"`
	AccessExpiresAt  time.Time        `json:"access_expires_at"`
	FamilyExpiresAt  time.Time        `json:"family_expires_at"`
}

func (nativeGrantResponse) Format(state fmt.State, _ rune) {
	_, _ = io.WriteString(state, "[native wire grant redacted]")
}

func (nativeGrantResponse) String() string   { return "[native wire grant redacted]" }
func (nativeGrantResponse) GoString() string { return "[native wire grant redacted]" }
func (nativeGrantResponse) LogValue() slog.Value {
	return slog.StringValue("[native wire grant redacted]")
}

func writeNativeGrant(w http.ResponseWriter, grant IssuedNative) {
	p := grant.Principal()
	if p.User().ID == uuid.Nil || p.FamilyID() == uuid.Nil || p.ClientInstanceID() == uuid.Nil || grant.RefreshSequence() > nativeMaxSequence || grant.access.wire() == "" || grant.refresh.wire() == "" {
		httpx.WriteError(w, httpx.ErrInternal("invalid native grant"))
		return
	}

	httpx.WriteJSON(w, http.StatusOK, nativeGrantResponse{User: grant.Principal().User(), FamilyID: p.FamilyID(), ClientInstanceID: p.ClientInstanceID(), RefreshSequence: grant.RefreshSequence(),
		AccessToken: nativeWireSecret{value: grant.access}, RefreshToken: nativeWireSecret{value: grant.refresh},
		AccessExpiresAt: grant.AccessExpiresAt(), FamilyExpiresAt: grant.FamilyExpiresAt()})
}

func nativeHandlerError(err error) error {
	var storeErr nativeStoreError
	if errors.As(err, &storeErr) {
		return err
	}
	if errors.Is(err, ErrNativeUnauthorized) {
		return httpx.ErrUnauthorized("authentication required")
	}
	if errors.Is(err, ErrNativeLimit) {
		return httpx.ErrConflict("native session limit reached")
	}
	if _, ok := httpx.AsAPIError(err); ok {
		return err
	}
	return nativeStoreFailure(err)
}

func (h *NativeHandler) login(w http.ResponseWriter, r *http.Request) error {
	if err := noNativeAuthorization(r); err != nil {
		return err
	}
	fields, err := decodeNativeStrings(w, r, "username", "password", "client_instance_id")
	if err != nil {
		return nativeHandlerError(err)
	}
	if len(fields["username"]) > 64 || len(fields["password"]) > MaxPasswordBytes {
		return httpx.ErrInvalidInput("invalid native credential fields")
	}
	instanceID, err := uuid.Parse(fields["client_instance_id"])
	if err != nil || instanceID == uuid.Nil || instanceID.String() != fields["client_instance_id"] {
		return httpx.ErrInvalidInput("invalid native client instance")
	}
	user, version, retry, err := h.accounts.verifyLogin(r, LoginRequest{Username: fields["username"], Password: fields["password"]})
	if retry > 0 {
		httpx.WriteRateLimited(w, retry)
		return nil
	}
	if err != nil {
		return nativeHandlerError(err)
	}
	grant, err := h.sessions.IssueVerified(r.Context(), verifiedNativeLogin{userID: user.ID, tokenVersion: version}, instanceID)
	if err != nil {
		return nativeHandlerError(err)
	}
	writeNativeGrant(w, grant)
	return nil
}

func (h *NativeHandler) refresh(w http.ResponseWriter, r *http.Request) error {
	if err := noNativeAuthorization(r); err != nil {
		return err
	}
	fields, err := decodeNativeStrings(w, r, "refresh_token")
	if err != nil {
		return err
	}
	grant, err := h.sessions.RotateRefresh(r.Context(), fields["refresh_token"])
	if errors.Is(err, ErrNativeRefreshWait) {
		httpx.WriteRateLimited(w, nativeRefreshInterval)
		return nil
	}
	if err != nil {
		return nativeHandlerError(err)
	}
	writeNativeGrant(w, grant)
	return nil
}

type nativePrincipalContextKey struct{}

func NativePrincipalFrom(ctx context.Context) (NativePrincipal, bool) {
	principal, ok := ctx.Value(nativePrincipalContextKey{}).(NativePrincipal)
	return principal, ok
}

func (h *NativeHandler) RequireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// This middleware remains safe when used outside Handler() as well.
		w.Header().Set("Cache-Control", "no-store")
		if nativeHeaderPresent(r, "Origin") || nativeHeaderPresent(r, "Cookie") || r.URL.RawQuery != "" || r.URL.User != nil || r.URL.Fragment != "" {
			httpx.WriteError(w, httpx.ErrForbidden("native transport request rejected"))
			return
		}
		encoded, err := nativeBearer(r)
		if err != nil {
			httpx.WriteError(w, nativeHandlerError(err))
			return
		}
		principal, err := h.sessions.AuthenticateAccess(r.Context(), encoded)
		if err != nil {
			httpx.WriteError(w, nativeHandlerError(err))
			return
		}
		ctx := context.WithValue(WithUser(r.Context(), &principal.user), nativePrincipalContextKey{}, principal)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (h *NativeHandler) me(w http.ResponseWriter, r *http.Request) error {
	if err := emptyNativeBody(r); err != nil {
		return err
	}
	principal, ok := NativePrincipalFrom(r.Context())
	if !ok {
		return httpx.ErrUnauthorized("authentication required")
	}
	httpx.WriteJSON(w, http.StatusOK, principal.User())
	return nil
}

func (h *NativeHandler) logout(w http.ResponseWriter, r *http.Request) error {
	if err := emptyNativeBody(r); err != nil {
		return err
	}
	principal, ok := NativePrincipalFrom(r.Context())
	if !ok {
		return httpx.ErrUnauthorized("authentication required")
	}
	if err := h.sessions.RevokeFamily(r.Context(), principal); err != nil {
		return nativeHandlerError(err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// AuthenticateNativeRequest shares the reviewed native header and target boundary.
// It returns an auth-owned lease for native socket admission, never a browser cookie.
func (h *NativeHandler) AuthenticateNativeRequest(r *http.Request) (NativeLease, error) {
	if nativeHeaderPresent(r, "Origin") || nativeHeaderPresent(r, "Cookie") || r.URL.RawQuery != "" || r.URL.User != nil || r.URL.Fragment != "" {
		return NativeLease{}, httpx.ErrForbidden("native transport request rejected")
	}
	encoded, err := nativeBearer(r)
	if err != nil {
		return NativeLease{}, nativeHandlerError(err)
	}
	lease, err := h.sessions.AuthenticateAccessLease(r.Context(), encoded)
	if err != nil {
		return NativeLease{}, nativeHandlerError(err)
	}
	return lease, nil
}

func (h *NativeHandler) RevalidateNativeLease(ctx context.Context, p NativePrincipal) (NativeLease, error) {
	return h.sessions.RevalidateNativeLease(ctx, p)
}
func (h *NativeHandler) AuthenticateAccessLease(ctx context.Context, encoded string) (NativeLease, error) {
	return h.sessions.AuthenticateAccessLease(ctx, encoded)
}
