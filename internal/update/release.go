package update

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/url"
	"strings"
	"time"
	"unicode"
)

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
	dec := json.NewDecoder(bytes.NewReader(body))
	var gr githubRelease
	if err := dec.Decode(&gr); err != nil {
		return nil, errors.New("GitHub's answer is not valid JSON")
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
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
	if releasePageRejected(u, err) {
		return fallback
	}
	return u.String()
}

func releaseBase(releaseURL string) *url.URL {
	base, err := url.Parse(releaseURL)
	if err != nil {
		slog.Debug("update check url", "err", err)
	}
	return base
}

func drainReleaseBody(body io.Reader) {
	if _, err := io.Copy(io.Discard, io.LimitReader(body, 64<<10)); err != nil {
		slog.Debug("update check drain", "err", err)
	}
}

func releasePageRejected(u *url.URL, err error) bool {
	if err != nil {
		return true
	}
	if u.Scheme != "https" || u.Host != "github.com" || u.User != nil {
		return true
	}
	if !strings.HasPrefix(u.EscapedPath(), "/"+Repo+"/releases/") || u.RawQuery != "" {
		return true
	}
	return u.Fragment != ""
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
