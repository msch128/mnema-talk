package update

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// updaterTimeout bounds one call to the sidecar. The sidecar checks the
// image's signature and provenance before it answers (a few registry round
// trips, at most 90 seconds on its side); the pull and restart happen after
// the answer (?async=true).
const updaterTimeout = 120 * time.Second

var (
	// ErrUpdaterBusy: the sidecar is already running an update.
	ErrUpdaterBusy = errors.New("an update is already running")
	// ErrUpdaterAuth: the sidecar rejected UPDATER_TOKEN.
	ErrUpdaterAuth = errors.New("the updater rejected the token; check UPDATER_TOKEN on both containers")
	// ErrUpdaterUntrusted: the sidecar examined the new image and it did not
	// pass the signature or provenance check; nothing was changed.
	ErrUpdaterUntrusted = errors.New("the updater refused the update: the new image's signature or build provenance did not verify; nothing was changed")
	// ErrUpdaterRequest: the sidecar did not understand the request (an
	// updater older than the app).
	ErrUpdaterRequest = errors.New("the updater did not accept the request; update the updater (docker compose --profile autoupdate up -d)")
	// ErrUpdaterUnverified: the sidecar could not carry the check out
	// (registry or Sigstore unreachable); nothing was changed.
	ErrUpdaterUnverified = errors.New("the updater could not verify the new image (registry or Sigstore unreachable); nothing was changed")
	// ErrUpdaterUnreachable: no answer (sidecar not started, wrong URL).
	ErrUpdaterUnreachable = errors.New("the updater is not reachable; is the autoupdate profile running?")
)

// Updater asks the optional updater sidecar (cmd/updater in front of a
// Watchtower, see docker-compose.yml) to update the containers it is scoped
// to: it verifies the new image's cosign signature and provenance, and only
// then pulls that digest and restarts the app. The app holds no Docker access:
// the sidecar alone decides what it may update (label + scope), the app can
// only say "now".
type Updater struct {
	endpoint string
	token    string
	client   *http.Client
}

func isValidUpdaterURL(u *url.URL) bool {
	if u.Host == "" || u.User != nil {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

// NewUpdater returns a client for the sidecar at baseURL (e.g.
// http://mnema-updater:8080), authenticated with token.
func NewUpdater(baseURL, token string) (*Updater, error) {
	u, err := url.Parse(strings.TrimRight(strings.TrimSpace(baseURL), "/"))
	if err != nil || !isValidUpdaterURL(u) {
		return nil, errors.New("UPDATER_URL must be an http(s) URL without credentials")
	}
	u.Path += "/v1/update"
	return &Updater{
		endpoint: u.String(),
		token:    token,
		client: &http.Client{
			Timeout: updaterTimeout,
			// The token must only ever go to the configured address.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}, nil
}

// Trigger starts an update. It never returns the sidecar's response body or
// the token in an error.
func (u *Updater) Trigger(ctx context.Context, targetVersion string) error {
	q := url.Values{"async": {"true"}, "target_version": {targetVersion}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.endpoint+"?"+q.Encode(), nil)
	if err != nil {
		return fmt.Errorf("build updater request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+u.token)
	req.Header.Set("User-Agent", "mnema-talk")
	res, err := u.client.Do(req)
	if err != nil {
		return ErrUpdaterUnreachable
	}
	defer res.Body.Close()
	if _, err := io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10)); err != nil {
		slog.Debug("updater response drain failed", "err", err)
	}
	switch res.StatusCode {
	case http.StatusOK, http.StatusAccepted:
		return nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return ErrUpdaterAuth
	case http.StatusTooManyRequests:
		return ErrUpdaterBusy
	case http.StatusUnprocessableEntity:
		return ErrUpdaterUntrusted
	case http.StatusBadRequest:
		return ErrUpdaterRequest
	case http.StatusBadGateway:
		return ErrUpdaterUnverified
	default:
		return fmt.Errorf("the updater answered HTTP %d", res.StatusCode)
	}
}
