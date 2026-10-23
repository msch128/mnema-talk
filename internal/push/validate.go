package push

import (
	"crypto/ecdh"
	"encoding/base64"
	"errors"
	"net/url"
	"strings"

	"github.com/msch128/mnema-talk/internal/httpx"
	"github.com/msch128/mnema-talk/internal/ssrf"
)

// maxEndpointLen is the longest endpoint the database stores.
const maxEndpointLen = 2048

// Refusal texts shown to members; they never carry the network detail.
const (
	msgEndpoint = "endpoint must be an https URL of a push service on a public address"
	msgKeys     = "p256dh must be a P-256 public key and auth a 16 byte secret, both base64url"
)

// ValidateEndpoint accepts only what a browser's push service address looks
// like: https on the default port, with a host that is not a literal private,
// loopback or reserved address. Names are checked again whenever the server
// connects (see ssrf.Dialer), so a name that later points inside the network
// is refused at send time, and the server is no use as a request proxy: the
// body is encrypted binary and the answer is never shown.
func ValidateEndpoint(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if len(raw) > maxEndpointLen {
		return "", httpx.ErrInvalidInput(msgEndpoint)
	}
	u, err := (&ssrf.Policy{}).ValidateURL(raw)
	if err != nil || !acceptablePushURL(u) {
		return "", httpx.ErrInvalidInput(msgEndpoint)
	}
	return u.String(), nil
}

func acceptablePushURL(u *url.URL) bool {
	if u.Scheme != "https" || u.Fragment != "" {
		return false
	}
	return u.Port() == "" || u.Port() == "443"
}

// ValidateKeys checks the browser's public key (a point on P-256) and auth
// secret, and returns both in canonical unpadded base64url.
func ValidateKeys(p256dh, auth string) (string, string, error) {
	pub, err := decodeKey(p256dh)
	if err != nil {
		return "", "", httpx.ErrInvalidInput(msgKeys)
	}
	if _, err := ecdh.P256().NewPublicKey(pub); err != nil {
		return "", "", httpx.ErrInvalidInput(msgKeys)
	}
	secret, err := decodeKey(auth)
	if err != nil || len(secret) != 16 {
		return "", "", httpx.ErrInvalidInput(msgKeys)
	}
	return base64.RawURLEncoding.EncodeToString(pub), base64.RawURLEncoding.EncodeToString(secret), nil
}

// decodeKey reads base64url with or without padding.
func decodeKey(s string) ([]byte, error) {
	s = strings.TrimRight(strings.TrimSpace(s), "=")
	if s == "" {
		return nil, errors.New("empty key")
	}
	return base64.RawURLEncoding.DecodeString(s)
}
