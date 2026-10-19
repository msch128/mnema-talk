package audit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"regexp"
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
// When they are too big the request part is trimmed first, then the others,
// but the changes (the diff) are always kept and the entry says what was
// trimmed (<key>_truncated).
func SanitizeDetails(details map[string]any) map[string]any {
	if details == nil {
		return map[string]any{}
	}
	out, _ := sanitize(details, 0).(map[string]any)
	for _, key := range []string{"request", "after", "before", "params"} {
		if size(out) <= maxDetailBytes {
			return out
		}
		if _, ok := out[key]; ok {
			delete(out, key)
			out[key+"_truncated"] = true
		}
	}
	if size(out) > maxDetailBytes {
		// Only a huge changes block or free-form fields remain.
		return map[string]any{"truncated": true}
	}
	return out
}

func size(v map[string]any) int {
	b, err := json.Marshal(v)
	if err != nil {
		return maxDetailBytes + 1
	}
	return len(b)
}

func sanitize(v any, depth int) any {
	if depth > maxDepth {
		return "…"
	}
	switch x := v.(type) {
	case map[string]any:
		return sanitizeMap(x, depth)
	case []any:
		return sanitizeList(x, depth)
	case Content:
		return clip(string(x), MaxContentRunes)
	case string:
		return cleanString(x)
	default:
		return v
	}
}

// sanitizeMap keeps at most maxItems entries and redacts secret values.
func sanitizeMap(m map[string]any, depth int) map[string]any {
	out := make(map[string]any, len(m))
	n := 0
	for k, val := range m {
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
}

func sanitizeList(list []any, depth int) []any {
	if len(list) > maxItems {
		list = list[:maxItems]
	}
	out := make([]any, len(list))
	for i, val := range list {
		out[i] = sanitize(val, depth+1)
	}
	return out
}

// cleanString reduces URLs to scheme and host plus a short hash of the whole
// address (so a change is still visible): paths can carry tokens too
// (https://host/hooks/<token>). Other strings are only bounded in length.
func cleanString(s string) string {
	return clip(urlPattern.ReplaceAllStringFunc(s, func(raw string) string {
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" {
			return "[url]"
		}
		sum := sha256.Sum256([]byte(raw))
		return strings.ToLower(u.Scheme) + "://" + u.Host + " [sha256:" + hex.EncodeToString(sum[:4]) + "]"
	}), maxStringRunes)
}

// urlPattern finds web addresses anywhere in a string, in any letter case.
var urlPattern = regexp.MustCompile(`(?i)https?://[^\s"'<>]+`)
