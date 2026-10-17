package audit

import (
	"encoding/json"
	"net/url"
	"strings"
)

const (
	maxStringRunes = 200
	// MaxContentRunes bounds message text kept for deletions and edits.
	MaxContentRunes = 4000
	maxItems        = 50
	maxDepth        = 5
	maxDetailBytes  = 32 << 10
	redacted        = "[redacted]"
)

// secretKeys are fragments of key names whose values are never stored.
var secretKeys = []string{"password", "token", "secret", "hash", "cookie", "authorization", "code", "session_id"}

func secretKey(k string) bool {
	k = strings.ToLower(k)
	for _, s := range secretKeys {
		if strings.Contains(k, s) {
			return true
		}
	}
	return false
}

// Content marks a string that may keep up to MaxContentRunes (message text).
type Content string

// SanitizeDetails returns details with secrets removed and sizes bounded.
func SanitizeDetails(details map[string]any) map[string]any {
	if details == nil {
		return map[string]any{}
	}
	out, _ := sanitize(details, 0).(map[string]any)
	if b, err := json.Marshal(out); err != nil || len(b) > maxDetailBytes {
		return map[string]any{"truncated": true}
	}
	return out
}

func sanitize(v any, depth int) any {
	if depth > maxDepth {
		return "…"
	}
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		n := 0
		for k, val := range x {
			if n++; n > maxItems {
				break
			}
			if secretKey(k) {
				out[k] = redacted
				continue
			}
			out[k] = sanitize(val, depth+1)
		}
		return out
	case []any:
		if len(x) > maxItems {
			x = x[:maxItems]
		}
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = sanitize(val, depth+1)
		}
		return out
	case Content:
		return clip(string(x), MaxContentRunes)
	case string:
		return cleanString(x)
	default:
		return v
	}
}

// cleanString drops credentials and queries from URLs and bounds the length.
func cleanString(s string) string {
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		if u, err := url.Parse(s); err == nil {
			u.User, u.RawQuery, u.Fragment = nil, "", ""
			s = u.String()
		}
	}
	return clip(s, maxStringRunes)
}
