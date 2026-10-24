// Package discordimport copies the structure of a Discord server (categories,
// channels, roles and their permission overwrites) into Mnema Talk from a
// public Discord server template link. It is strictly additive and fully
// undoable (doc/import.md).
package discordimport

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/ssrf"
)

const (
	// templateAPI is the only place a template is ever fetched from. The host
	// is fixed: a pasted link only ever contributes its template code.
	templateAPI = "https://discord.com/api/v10/guilds/templates/"
	// MaxTemplateBytes caps the template document we are willing to read.
	MaxTemplateBytes = 2 << 20
	fetchTimeout     = 10 * time.Second

	maxTemplateChannels = 600
	maxTemplateRoles    = 300
)

var codePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{2,40}$`)

// linkHosts are the hosts of template links we accept pasted.
var linkHosts = map[string]bool{
	"discord.new": true, "discord.com": true, "www.discord.com": true,
	"discordapp.com": true, "www.discordapp.com": true,
}

var errBadLink = httpx.ErrInvalidInput("this is not a Discord server template link or code")

// ParseCode extracts the template code from a pasted discord.new/<code> or
// discord.com/template/<code> link, or accepts a bare code.
func ParseCode(input string) (string, error) {
	s := strings.TrimSpace(input)
	if codePattern.MatchString(s) {
		return s, nil
	}
	if s == "" || len(s) > 300 || strings.ContainsAny(s, " \t\r\n\x00") {
		return "", errBadLink
	}
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return "", errBadLink
	}
	host := strings.ToLower(u.Hostname())
	if !linkHosts[host] {
		return "", errBadLink
	}
	path := strings.Trim(u.Path, "/")
	if host != "discord.new" {
		rest, ok := strings.CutPrefix(path, "template/")
		if !ok {
			return "", errBadLink
		}
		path = rest
	}
	if !codePattern.MatchString(path) {
		return "", errBadLink
	}
	return path, nil
}

// flexNum is a JSON value Discord sends as a string or a number (ids and
// permission bit fields), kept as its decimal digits.
type flexNum string

func (f *flexNum) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	if s == "null" || s == "" {
		*f = ""
		return nil
	}
	s = strings.Trim(s, `"`)
	if _, err := strconv.ParseUint(s, 10, 64); err != nil {
		return errors.New("not a number")
	}
	*f = flexNum(s)
	return nil
}

// Bits reads the value as a bit field; garbage counts as empty.
func (f flexNum) Bits() uint64 {
	n, _ := strconv.ParseUint(string(f), 10, 64)
	return n
}

// Template is the part of Discord's template document the import reads.
type Template struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Guild       Guild  `json:"serialized_source_guild"`
}

// Guild is the serialized source server of a template.
type Guild struct {
	Name     string    `json:"name"`
	Roles    []SrcRole `json:"roles"`
	Channels []SrcChan `json:"channels"`
}

// SrcRole is a role of the source server.
type SrcRole struct {
	ID          flexNum `json:"id"`
	Name        string  `json:"name"`
	Permissions flexNum `json:"permissions"`
	Color       int     `json:"color"`
	Hoist       bool    `json:"hoist"`
	Mentionable bool    `json:"mentionable"`
	Position    int     `json:"position"`
}

// SrcChan is a channel or category of the source server.
type SrcChan struct {
	ID        flexNum        `json:"id"`
	Type      int            `json:"type"`
	Name      string         `json:"name"`
	Topic     *string        `json:"topic"`
	Position  int            `json:"position"`
	ParentID  flexNum        `json:"parent_id"`
	UserLimit int            `json:"user_limit"`
	RateLimit int            `json:"rate_limit_per_user"`
	NSFW      bool           `json:"nsfw"`
	Overwrite []SrcOverwrite `json:"permission_overwrites"`
}

// SrcOverwrite is a permission overwrite of a source channel.
type SrcOverwrite struct {
	ID    flexNum         `json:"id"`
	Type  json.RawMessage `json:"type"`
	Allow flexNum         `json:"allow"`
	Deny  flexNum         `json:"deny"`
}

// forRole reports an overwrite that targets a role (type 0 or "role").
func (o SrcOverwrite) forRole() bool {
	t := strings.Trim(string(o.Type), `" `)
	return t == "0" || t == "role"
}

// Fetcher downloads templates from Discord.
type Fetcher struct {
	base   string
	client *http.Client
}

// NewFetcher returns a Fetcher that reaches Discord through the SSRF guard:
// public addresses only, no proxy, no redirects, a short timeout.
func NewFetcher() *Fetcher {
	return &Fetcher{base: templateAPI, client: ssrf.NewClient(ssrf.NewDialer(&ssrf.Policy{}), fetchTimeout)}
}

var (
	errNotFound    = httpx.ErrNotFound("this template does not exist or is no longer shared")
	errUnreachable = httpx.ErrUnavailable("Discord could not be reached; try again later")
	errTooLarge    = httpx.ErrPayloadTooLarge("the template is too large to import")
	errBadTemplate = httpx.ErrInvalidInput("Discord sent something that is not a server template")
)

// Fetch downloads the template for a code that ParseCode accepted. Callers see
// only the sanitized errors above, never the raw network error.
func (f *Fetcher) Fetch(ctx context.Context, code string) (*Template, error) {
	if !codePattern.MatchString(code) {
		return nil, errBadLink
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.base+code, nil)
	if err != nil {
		return nil, errBadLink
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "MnemaTalk-Import")
	resp, err := f.client.Do(req)
	if err != nil {
		return nil, errUnreachable
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, errNotFound
	case resp.StatusCode != http.StatusOK: // includes redirects: never followed
		return nil, errUnreachable
	case resp.ContentLength > MaxTemplateBytes:
		return nil, errTooLarge
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxTemplateBytes+1))
	if err != nil {
		return nil, errUnreachable
	}
	if len(body) > MaxTemplateBytes {
		return nil, errTooLarge
	}
	var t Template
	if err := json.Unmarshal(body, &t); err != nil || len(t.Guild.Channels)+len(t.Guild.Roles) == 0 {
		return nil, errBadTemplate
	}
	if len(t.Guild.Channels) > maxTemplateChannels || len(t.Guild.Roles) > maxTemplateRoles {
		return nil, errTooLarge
	}
	t.Code = code
	return &t, nil
}
