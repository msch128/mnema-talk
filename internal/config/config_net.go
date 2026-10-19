package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// Placeholder values shipped in .env.example (and older versions of it). They
// are public, so they must never be accepted as real secrets. Any value
// starting with "replace_with_" counts as a placeholder as well.
var examplePlaceholders = map[string]bool{
	"replace_with_a_secure_random_string_in_production": true,
	"change_this_password_immediately":                  true,
	"replace_with_access_key":                           true,
	"replace_with_secret_key":                           true,
	"replace_with_a_random_database_password":           true,
}

// isPlaceholder reports whether v is one of the public example values.
func isPlaceholder(v string) bool {
	return examplePlaceholders[v] || strings.HasPrefix(strings.ToLower(v), "replace_with_")
}

// defaultTrustedProxies is loopback only: a reverse proxy on the same host.
// A proxy in a Docker network or elsewhere in the LAN must be listed
// explicitly in TRUSTED_PROXY_CIDRS, otherwise any host in those private
// ranges could spoof X-Forwarded-For.
var defaultTrustedProxies = "127.0.0.0/8,::1/128"

// IsLoopbackAddr reports whether host is "localhost" or a loopback IP.
func IsLoopbackAddr(host string) bool {
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// resolveBindAddr applies the BIND_ADDR default and the development rule.
// Development logs the generated admin password and uses a random JWT
// secret, so it must never face a network: it defaults to loopback and
// refuses any other address. All other environments default to 0.0.0.0.
func (c *Config) resolveBindAddr() error {
	dev := c.AppEnv == "development" || c.AppEnv == "dev"
	if c.BindAddr == "" {
		if dev {
			c.BindAddr = "127.0.0.1"
		} else {
			c.BindAddr = "0.0.0.0"
		}
		return nil
	}
	if dev && !IsLoopbackAddr(c.BindAddr) {
		return fmt.Errorf("BIND_ADDR=%s is not allowed with APP_ENV=development: development logs the generated admin password and uses a random JWT secret, so it may only listen on loopback; set APP_ENV=production (with real secrets) to listen on a network address", c.BindAddr)
	}
	return nil
}

// BroadTrustedProxies lists TRUSTED_PROXY_CIDRS entries that are not
// loopback and cover more than a small network (IPv4 shorter than /24, IPv6
// shorter than /64). Any host in such a range can forge X-Forwarded-For.
func (c *Config) BroadTrustedProxies() []string {
	var out []string
	for _, p := range c.TrustedProxies {
		_, n, err := net.ParseCIDR(p)
		if err != nil || n.IP.IsLoopback() {
			continue
		}
		ones, bits := n.Mask.Size()
		if (bits == 32 && ones < 24) || (bits == 128 && ones < 64) {
			out = append(out, p)
		}
	}
	return out
}

// RandomHex returns a cryptographically random hex string of n bytes.
func RandomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b) // never fails since Go 1.24: it aborts the process instead
	return hex.EncodeToString(b)
}

// normalizeURL trims the URL and a trailing slash, lower-cases scheme and host
// and drops the default port (:443 for https, :80 for http), so it compares
// equal to the Origin header browsers send. Unparsable input is returned
// trimmed; validation reports it.
func normalizeURL(raw string) string {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return raw
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	if p := u.Port(); (u.Scheme == "https" && p == "443") || (u.Scheme == "http" && p == "80") {
		u.Host = strings.TrimSuffix(u.Host, ":"+p)
	}
	return strings.TrimRight(u.String(), "/")
}

// SplitList splits a comma-separated list, dropping empty entries.
func SplitList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}
