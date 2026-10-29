// Package setupwizard implements `mnema-talk setup`: it asks for the public
// address (and optionally mail), generates the secrets, writes a complete
// .env and docker-compose.yml into a directory and runs the preflight checks
// that can be done from inside a container.
package setupwizard

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"strings"

	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/config"
	"github.com/msch128/mnema-talk/internal/version"
)

// DefaultAdminUsername is the Owner name the server seeds when none is set.
const DefaultAdminUsername = "admin"

// DefaultLanguage is the documented default of INITIAL_LANGUAGE (doc/operations.md,
// .env.example and the server itself): the wizard asks with the same one.
const DefaultLanguage = "de"

// LocalURL is PUBLIC_URL of a trial on one computer.
const LocalURL = "http://localhost:8080"

// defaultAppPort is the host port the web app is published on (PORT).
const defaultAppPort = "8080"

// ImageRepository is the published image.
const ImageRepository = "ghcr.io/msch128/mnema-talk"

// SMTP is the optional outgoing mail setup.
type SMTP struct {
	Host     string
	Port     string
	TLS      string // starttls (default), tls or plain
	Username string
	Password string
	From     string
}

// Enabled reports whether mail is set up.
func (s SMTP) Enabled() bool { return s.Host != "" }

// Options is everything the generated files depend on.
type Options struct {
	// Dir receives .env and docker-compose.yml.
	Dir string
	// PublicURL is the address members open, e.g. https://chat.example.com.
	PublicURL string
	// Image is the full image reference; empty derives the tag from this build.
	Image string
	// Language is the language of the default channel names, en or de.
	Language string
	// AdminUsername is the Owner's user name.
	AdminUsername string
	SMTP          SMTP
	// Local sets up a trial on this computer: PUBLIC_URL is http://localhost:8080
	// unless PublicURL says otherwise, and nothing is published to the network.
	Local bool
	// LANIP is this host's address in the local network (WEBRTC_LAN_IP); empty
	// when unknown.
	LANIP string
	// CheckOnly checks an existing .env and writes nothing.
	CheckOnly bool
	// Start runs docker compose up -d --wait and the live checks afterwards.
	Start bool
	// AutoUpdate adds the updater sidecar profile and its token.
	AutoUpdate bool
	// Force replaces existing files; see the warning in Generate.
	Force bool
	// SkipPreflight skips the checks that touch the network.
	SkipPreflight bool
	// TemplatePath is the docker-compose.yml to derive the generated one from.
	TemplatePath string
}

// defaultImage names the image of this build: its major.minor tag follows
// patch releases, a pre-release is pinned exactly and a development build
// follows latest.
func defaultImage(v string) string {
	parts := strings.SplitN(v, ".", 3)
	switch {
	case len(parts) < 3:
		return ImageRepository + ":latest"
	case strings.ContainsAny(parts[2], "-+"):
		return ImageRepository + ":" + v
	}
	return ImageRepository + ":" + parts[0] + "." + parts[1]
}

// normalize fills defaults and validates; the returned Options is a copy.
func (o Options) normalize() (Options, error) {
	if o.Local && o.PublicURL == "" {
		o.PublicURL = LocalURL
	}
	pu, err := NormalizePublicURL(o.PublicURL)
	if err != nil {
		return o, err
	}
	o.PublicURL = pu
	if o.Image == "" {
		o.Image = defaultImage(version.Current())
	}
	if o.Language == "" {
		o.Language = DefaultLanguage
	}
	if o.Language != "en" && o.Language != "de" {
		return o, fmt.Errorf("language must be en or de, got %q", o.Language)
	}
	if o.AdminUsername == "" {
		o.AdminUsername = DefaultAdminUsername
	}
	// The server refuses to boot with a name it cannot seed, and a newline
	// would add lines to the .env.
	if err := auth.ValidateAdminSeed(o.AdminUsername, ""); err != nil {
		return o, fmt.Errorf("--admin-username: %w", err)
	}
	if o.Dir == "" {
		o.Dir = "."
	}
	if err := validateLANIP(o.LANIP); err != nil {
		return o, err
	}
	return o, o.validateSMTP()
}

// validateLANIP accepts an empty value or the IPv4 address of this host in the
// local network.
func validateLANIP(v string) error {
	if v == "" {
		return nil
	}
	a, err := netip.ParseAddr(v)
	if err != nil || !a.Is4() || !a.IsGlobalUnicast() {
		return fmt.Errorf("--lan-ip: %q is not the IPv4 address of this machine on your network, e.g. 192.168.1.20", v)
	}
	return nil
}

// IsLocal reports whether PUBLIC_URL names this computer only.
func (o Options) IsLocal() bool {
	u, err := url.Parse(o.PublicURL)
	return err == nil && config.IsLoopbackAddr(u.Hostname())
}

// AppPort is the host port the web app is published on: the one in a local
// PUBLIC_URL (http://localhost:9000), else the default 8080.
func (o Options) AppPort() string {
	if u, err := url.Parse(o.PublicURL); err == nil && o.IsLocal() && u.Port() != "" {
		return u.Port()
	}
	return defaultAppPort
}

func (o Options) validateSMTP() error {
	s := o.SMTP
	if !s.Enabled() {
		return nil
	}
	if s.From == "" {
		return errors.New("mail needs a sender address (--smtp-from)")
	}
	switch s.TLS {
	case "", "starttls", "tls", "plain":
	default:
		return fmt.Errorf("--smtp-tls must be starttls, tls or plain, got %q", s.TLS)
	}
	return nil
}

// NormalizePublicURL turns what an operator types into the form PUBLIC_URL
// expects: a bare host name becomes https://host, a trailing slash goes away.
// Only a scheme and host (and port) are allowed, no path.
func NormalizePublicURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("the public URL is required, e.g. https://chat.example.com")
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", fmt.Errorf("%q is not an http(s) address like https://chat.example.com", raw)
	}
	if (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return "", fmt.Errorf("%q must be only scheme and host (no path or credentials)", raw)
	}
	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host), nil
}
