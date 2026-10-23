package updaterd

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Labels that put a container in the updater's scope (docker-compose.yml sets
// them on the app only).
const (
	labelEnable = "com.centurylinklabs.watchtower.enable=true"
	labelScope  = "com.centurylinklabs.watchtower.scope="
)

// Target is a running container the updater may update.
type Target struct {
	Name string
	// RunningVersion is the release the container runs, from its image's
	// org.opencontainers.image.version label ("" if it has none).
	RunningVersion string
	// Image is the reference the container was created from, e.g.
	// "ghcr.io/msch128/mnema-talk:1.0".
	Image string
}

// Engine is the part of the Docker Engine API the updater uses. It pulls an
// image and moves a local tag; creating and replacing containers stays with the
// recreator (Watchtower).
type Engine interface {
	Targets(ctx context.Context, scope string) ([]Target, error)
	// Pull fetches repository@digest; the daemon checks the content hash.
	Pull(ctx context.Context, repository, digest string) error
	// Tag makes repository:tag point at the already pulled repository@digest.
	Tag(ctx context.Context, repository, digest, tag string) error
}

// Docker talks to the Engine API over its Unix socket.
type Docker struct {
	hc *http.Client
}

// NewDocker returns an Engine client for the socket at path.
func NewDocker(socket string) *Docker {
	return &Docker{hc: &http.Client{Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		},
		ResponseHeaderTimeout: 2 * time.Minute,
	}}}
}

func (d *Docker) do(ctx context.Context, method, path string, q url.Values) (*http.Response, error) {
	u := url.URL{Scheme: "http", Host: "docker", Path: path, RawQuery: q.Encode()}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), nil)
	if err != nil {
		return nil, err
	}
	return d.hc.Do(req)
}

// engineError reads a Docker error body without echoing more than a line.
func engineError(res *http.Response) error {
	var body struct {
		Message string `json:"message"`
	}
	_ = json.NewDecoder(io.LimitReader(res.Body, 4<<10)).Decode(&body)
	msg := strings.TrimSpace(body.Message)
	if len(msg) > 200 {
		msg = msg[:200]
	}
	return fmt.Errorf("docker answered HTTP %d: %s", res.StatusCode, msg)
}

// Targets lists the running containers carrying both labels and the image
// reference each was created from.
func (d *Docker) Targets(ctx context.Context, scope string) ([]Target, error) {
	filters, err := json.Marshal(map[string][]string{"label": {labelEnable, labelScope + scope}, "status": {"running"}})
	if err != nil {
		return nil, err
	}
	res, err := d.do(ctx, http.MethodGet, "/containers/json", url.Values{"filters": {string(filters)}})
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, engineError(res)
	}
	var list []struct {
		ID    string   `json:"Id"`
		Names []string `json:"Names"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(&list); err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}
	out := make([]Target, 0, len(list))
	for _, c := range list {
		image, version, err := d.configImage(ctx, c.ID)
		if err != nil {
			return nil, err
		}
		name := c.ID
		if len(c.Names) > 0 {
			name = strings.TrimPrefix(c.Names[0], "/")
		}
		out = append(out, Target{Name: name, Image: image, RunningVersion: version})
	}
	return out, nil
}

// configImage returns the reference a container was created from. The list
// call shows the image id once the tag moved on, so inspect is needed.
func (d *Docker) configImage(ctx context.Context, id string) (image, version string, err error) {
	res, err := d.do(ctx, http.MethodGet, "/containers/"+url.PathEscape(id)+"/json", nil)
	if err != nil {
		return "", "", fmt.Errorf("inspect container: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", "", engineError(res)
	}
	var c struct {
		Config struct {
			Image  string            `json:"Image"`
			Labels map[string]string `json:"Labels"`
		} `json:"Config"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(&c); err != nil {
		return "", "", fmt.Errorf("inspect container: %w", err)
	}
	return c.Config.Image, c.Config.Labels["org.opencontainers.image.version"], nil
}

// Pull implements Engine. The pull is a stream of JSON progress messages; an
// error is reported inside the stream with a 200 status, so it is read to the
// end.
func (d *Docker) Pull(ctx context.Context, repository, digest string) error {
	res, err := d.do(ctx, http.MethodPost, "/images/create", url.Values{"fromImage": {repository}, "tag": {digest}})
	if err != nil {
		return fmt.Errorf("pull %s@%s: %w", repository, digest, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return engineError(res)
	}
	sc := bufio.NewScanner(res.Body)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		var msg struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(sc.Bytes(), &msg) == nil && msg.Error != "" {
			return fmt.Errorf("pull %s@%s: %s", repository, digest, msg.Error)
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("pull %s@%s: %w", repository, digest, err)
	}
	return nil
}

// Tag implements Engine.
func (d *Docker) Tag(ctx context.Context, repository, digest, tag string) error {
	if repository == "" || digest == "" || tag == "" {
		return errors.New("tag: repository, digest and tag are required")
	}
	res, err := d.do(ctx, http.MethodPost, "/images/"+repository+"@"+digest+"/tag", url.Values{"repo": {repository}, "tag": {tag}})
	if err != nil {
		return fmt.Errorf("tag %s:%s: %w", repository, tag, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		return engineError(res)
	}
	return nil
}
