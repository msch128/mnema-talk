package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	// Repo is the GitHub repository whose releases are checked.
	Repo = "msch128/mnema-talk"
	// DefaultReleaseURL is GitHub's "latest release" endpoint for Repo. The
	// request is unauthenticated and carries no user data; GitHub sees the
	// server's IP address and the User-Agent "mnema-talk/<version>".
	DefaultReleaseURL = "https://api.github.com/repos/" + Repo + "/releases/latest"

	// Interval is how often the background loop checks. With conditional
	// requests (ETag) an unchanged answer is a 304, and 2 requests per hour
	// stay far below GitHub's unauthenticated limit of 60 per hour and IP.
	Interval = 30 * time.Minute
	// ManualMinGap is the least time between two checks an admin triggers
	// with "check now".
	ManualMinGap = time.Minute

	requestTimeout = 10 * time.Second
	// maxResponseBytes caps the JSON read from GitHub. A release object with
	// generous notes is a few KiB; GitHub itself limits bodies to 125k chars.
	maxResponseBytes = 1 << 20
	// maxNotesRunes caps the release notes kept in memory and shown to admins.
	maxNotesRunes = 20000
	// defaultBackoff applies to 403/429 answers without a usable reset time;
	// maxBackoff bounds any reset time GitHub announces.
	defaultBackoff = time.Hour
	maxBackoff     = 6 * time.Hour
)

var (
	// ErrBackoff means GitHub asked us to slow down and the wait is not over.
	ErrBackoff = errors.New("update check paused after a rate-limit answer from GitHub")
	// ErrTooSoon means a manual check came within ManualMinGap of the last one.
	ErrTooSoon = errors.New("update check ran moments ago")
)

// Release is the validated part of a GitHub release.
type Release struct {
	// Version is the tag without its leading "v", e.g. "0.4.0".
	Version string
	// URL is the release page on github.com.
	URL string
	// Notes is the release body (Markdown), trimmed and capped; it is shown
	// through the web app's safe Markdown renderer, never as HTML.
	Notes       string
	PublishedAt time.Time
}

// Snapshot is the checker's current knowledge.
type Snapshot struct {
	Latest    *Release
	CheckedAt time.Time
	// Error is a short, user-presentable reason why the last check failed.
	Error string
	// RetryAt is set while a GitHub rate limit pauses the checks.
	RetryAt time.Time
}

// Checker polls the latest release. It is safe for concurrent use; checks
// themselves are serialised.
type Checker struct {
	url       string
	userAgent string
	client    *http.Client
	now       func() time.Time

	checkMu sync.Mutex // one check at a time

	mu          sync.Mutex
	etag        string
	latest      *Release
	checkedAt   time.Time
	lastAttempt time.Time
	lastErr     string
	retryAt     time.Time
}

// NewChecker returns a checker for releaseURL (DefaultReleaseURL in
// production; tests pass an httptest server). current is the running
// version, sent in the User-Agent.
func NewChecker(releaseURL, current string) *Checker {
	base, _ := url.Parse(releaseURL)
	return &Checker{
		url:       releaseURL,
		userAgent: "mnema-talk/" + current + " (update check; +https://github.com/" + Repo + ")",
		now:       time.Now,
		client: &http.Client{
			Timeout: requestTimeout,
			// Follow at most a couple of redirects, and only on the same
			// scheme and host (GitHub answers 301 for a renamed repository).
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 3 {
					return errors.New("too many redirects")
				}
				if base == nil || req.URL.Scheme != base.Scheme || req.URL.Host != base.Host {
					return errors.New("redirect to another host refused")
				}
				return nil
			},
		},
	}
}

// Snapshot returns what the last checks found.
func (c *Checker) Snapshot() Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := Snapshot{CheckedAt: c.checkedAt, Error: c.lastErr}
	if c.latest != nil {
		r := *c.latest
		s.Latest = &r
	}
	if c.now().Before(c.retryAt) {
		s.RetryAt = c.retryAt
	}
	return s
}

// Run checks after initialDelay and then every Interval until ctx ends.
func (c *Checker) Run(ctx context.Context, initialDelay time.Duration) {
	t := time.NewTimer(initialDelay)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if err := c.Check(ctx); err != nil {
			slog.Debug("update check failed", "err", err)
		}
		t.Reset(Interval)
	}
}

// CheckNow is an admin's "check now": like Check, but refused within
// ManualMinGap of the previous attempt.
func (c *Checker) CheckNow(ctx context.Context) error {
	c.mu.Lock()
	tooSoon := !c.lastAttempt.IsZero() && c.now().Sub(c.lastAttempt) < ManualMinGap
	c.mu.Unlock()
	if tooSoon {
		return ErrTooSoon
	}
	return c.Check(ctx)
}

// Check asks GitHub for the latest release once. While a rate limit from an
// earlier answer is in force it returns ErrBackoff without any request.
func (c *Checker) Check(ctx context.Context) error {
	c.checkMu.Lock()
	defer c.checkMu.Unlock()

	c.mu.Lock()
	now := c.now()
	if now.Before(c.retryAt) {
		c.mu.Unlock()
		return ErrBackoff
	}
	c.lastAttempt = now
	etag := c.etag
	c.mu.Unlock()

	rel, newETag, notModified, retryAt, err := c.fetch(ctx, etag)

	c.mu.Lock()
	defer c.mu.Unlock()
	if !retryAt.IsZero() {
		c.retryAt = retryAt
	}
	if err != nil {
		c.lastErr = err.Error()
		slog.Debug("update check", "result", "error", "err", err)
		return err
	}
	c.lastErr = ""
	c.checkedAt = c.now()
	if notModified {
		slog.Debug("update check", "result", "not modified")
		return nil
	}
	c.latest = rel
	c.etag = newETag
	slog.Debug("update check", "result", "ok", "latest", rel.Version)
	return nil
}

// fetch performs one request. Errors are short and safe to show to admins
// (no response bodies, no internal addresses).
func (c *Checker) fetch(ctx context.Context, etag string) (rel *Release, newETag string, notModified bool, retryAt time.Time, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return nil, "", false, time.Time{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", c.userAgent)
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	res, err := c.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, "", false, time.Time{}, ctx.Err()
		}
		var ue *url.Error
		if errors.As(err, &ue) && ue.Timeout() {
			return nil, "", false, time.Time{}, errors.New("GitHub did not answer in time")
		}
		return nil, "", false, time.Time{}, errors.New("GitHub is not reachable")
	}
	defer res.Body.Close()
	// Drain a little so the connection can be reused, never more than the cap.
	defer func() { _, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10)) }()

	switch {
	case res.StatusCode == http.StatusNotModified:
		return nil, "", true, time.Time{}, nil
	case res.StatusCode == http.StatusForbidden || res.StatusCode == http.StatusTooManyRequests:
		return nil, "", false, c.backoffUntil(res.Header), fmt.Errorf("GitHub rate limit reached (HTTP %d)", res.StatusCode)
	case res.StatusCode == http.StatusNotFound:
		return nil, "", false, time.Time{}, errors.New("no published release found")
	case res.StatusCode != http.StatusOK:
		return nil, "", false, time.Time{}, fmt.Errorf("unexpected answer from GitHub (HTTP %d)", res.StatusCode)
	}

	if mt, _, err := mime.ParseMediaType(res.Header.Get("Content-Type")); err != nil || (mt != "application/json" && !strings.HasSuffix(mt, "+json")) {
		return nil, "", false, time.Time{}, errors.New("GitHub answered with something other than JSON")
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, maxResponseBytes+1))
	if err != nil {
		return nil, "", false, time.Time{}, errors.New("reading GitHub's answer failed")
	}
	if len(body) > maxResponseBytes {
		return nil, "", false, time.Time{}, errors.New("GitHub's answer is too large")
	}
	rel, err = parseRelease(body)
	if err != nil {
		return nil, "", false, time.Time{}, err
	}
	return rel, res.Header.Get("ETag"), false, time.Time{}, nil
}

// backoffUntil reads Retry-After or, for an exhausted quota, X-RateLimit-Reset.
func (c *Checker) backoffUntil(h http.Header) time.Time {
	now := c.now()
	wait := defaultBackoff
	if s, err := strconv.Atoi(strings.TrimSpace(h.Get("Retry-After"))); err == nil && s > 0 {
		wait = time.Duration(s) * time.Second
	} else if h.Get("X-RateLimit-Remaining") == "0" {
		if reset, err := strconv.ParseInt(strings.TrimSpace(h.Get("X-RateLimit-Reset")), 10, 64); err == nil {
			if d := time.Unix(reset, 0).Sub(now); d > 0 {
				wait = d
			}
		}
	}
	if wait > maxBackoff {
		wait = maxBackoff
	}
	return now.Add(wait)
}

// githubRelease lists the only fields we read; everything else is ignored.
type githubRelease struct {
	TagName     *string `json:"tag_name"`
	HTMLURL     string  `json:"html_url"`
	PublishedAt string  `json:"published_at"`
	Body        string  `json:"body"`
	Draft       bool    `json:"draft"`
	Prerelease  bool    `json:"prerelease"`
}

// parseRelease validates GitHub's answer strictly: one JSON object, a semver
// tag, a release page on github.com for Repo (else a link built from the
// tag), sanitized notes.
func parseRelease(body []byte) (*Release, error) {
	dec := json.NewDecoder(strings.NewReader(string(body)))
	var gr githubRelease
	if err := dec.Decode(&gr); err != nil {
		return nil, errors.New("GitHub's answer is not valid JSON")
	}
	if dec.More() {
		return nil, errors.New("GitHub's answer has trailing data")
	}
	if gr.TagName == nil {
		return nil, errors.New("GitHub's answer has no tag_name")
	}
	if gr.Draft || gr.Prerelease {
		return nil, errors.New("the latest release is a draft or pre-release")
	}
	v, ok := ParseSemver(*gr.TagName)
	if !ok {
		return nil, errors.New("the latest release tag is not a version number")
	}
	rel := &Release{Version: v.String(), Notes: cleanNotes(gr.Body)}
	rel.URL = releasePage(gr.HTMLURL, v)
	if t, err := time.Parse(time.RFC3339, gr.PublishedAt); err == nil {
		rel.PublishedAt = t.UTC()
	}
	return rel, nil
}

// releasePage accepts html_url only as https://github.com/<Repo>/releases/...;
// anything else is replaced by the tag page, so the admin UI never links to
// a host GitHub's answer chose.
func releasePage(raw string, v Semver) string {
	fallback := "https://github.com/" + Repo + "/releases/tag/v" + v.String()
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "github.com" || u.User != nil ||
		!strings.HasPrefix(u.EscapedPath(), "/"+Repo+"/releases/") || u.RawQuery != "" || u.Fragment != "" {
		return fallback
	}
	return u.String()
}

// cleanNotes drops control characters (except newlines and tabs) and
// invalid UTF-8, and caps the length.
func cleanNotes(s string) string {
	s = strings.ToValidUTF8(s, "")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	var b strings.Builder
	n := 0
	for _, r := range s {
		if n >= maxNotesRunes {
			b.WriteString("\n…")
			break
		}
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			continue
		}
		b.WriteRune(r)
		n++
	}
	return strings.TrimSpace(b.String())
}
