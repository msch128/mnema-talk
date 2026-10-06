package update

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// updaterTimeout bounds one call to the sidecar. The call is asynchronous on
// the sidecar's side (?async=true), so it returns as soon as the update was
// accepted; the pull and restart happen afterwards.
const updaterTimeout = 15 * time.Second

var (
	// ErrUpdaterBusy: the sidecar is already running an update.
	ErrUpdaterBusy = errors.New("an update is already running")
	// ErrUpdaterAuth: the sidecar rejected UPDATER_TOKEN.
	ErrUpdaterAuth = errors.New("the updater rejected the token; check UPDATER_TOKEN on both containers")
	// ErrUpdaterUnreachable: no answer (sidecar not started, wrong URL).
	ErrUpdaterUnreachable = errors.New("the updater is not reachable; is the autoupdate profile running?")
)

// Updater asks the optional updater sidecar (a Watchtower with only its
// HTTP update endpoint enabled, see docker-compose.yml) to re-pull and
// restart the containers it is scoped to. The app holds no Docker access:
// the sidecar alone decides what it may update (label + scope), the app can
// only say "now".
type Updater struct {
	endpoint string
	token    string
	client   *http.Client
}

// NewUpdater returns a client for the sidecar at baseURL (e.g.
// http://mnema-updater:8080), authenticated with token.
func NewUpdater(baseURL, token string) (*Updater, error) {
	u, err := url.Parse(strings.TrimRight(strings.TrimSpace(baseURL), "/"))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return nil, errors.New("UPDATER_URL must be an http(s) URL without credentials")
	}
	u.Path += "/v1/update"
	u.RawQuery = "async=true"
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
func (u *Updater) Trigger(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.endpoint, nil)
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
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))
	switch res.StatusCode {
	case http.StatusOK, http.StatusAccepted:
		return nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return ErrUpdaterAuth
	case http.StatusTooManyRequests:
		return ErrUpdaterBusy
	default:
		return fmt.Errorf("the updater answered HTTP %d", res.StatusCode)
	}
}
