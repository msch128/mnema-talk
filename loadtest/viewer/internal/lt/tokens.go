package lt

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
)

// Token is one load-test user's session.
type Token struct {
	ID    string `json:"id"`
	Token string `json:"token"`
}

// LoadTokens reads a JSON file [{"id": "...", "token": "..."}].
func LoadTokens(path string) ([]Token, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("TOKENS: %w", err)
	}
	var tokens []Token
	if err := json.Unmarshal(raw, &tokens); err != nil {
		return nil, fmt.Errorf("TOKENS: %w", err)
	}
	return tokens, nil
}

// CookieName is the session cookie the server expects: the __Host- variant
// when PUBLIC_URL is https, which is the scheme of base unless a reverse proxy
// hides it. SESSION_COOKIE overrides the guess for a server reached over plain
// http whose PUBLIC_URL is https.
func CookieName(base *url.URL) string {
	if v := os.Getenv("SESSION_COOKIE"); v != "" {
		return v
	}
	if base.Scheme == "https" {
		return "__Host-mnema_session"
	}
	return "mnema_session"
}

// ParseBase parses BASE_URL.
func ParseBase(raw string) (*url.URL, error) {
	base, err := url.Parse(raw)
	if err != nil || base.Host == "" || (base.Scheme != "http" && base.Scheme != "https") {
		return nil, fmt.Errorf("BASE_URL must be an http:// or https:// URL")
	}
	return base, nil
}
