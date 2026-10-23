package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// DefaultMaxBody is the global request body ceiling; upload routes override it.
const DefaultMaxBody int64 = 1 << 20 // 1 MiB

// DecodeJSON strictly decodes a single JSON object into dst: unknown fields,
// trailing data and oversized bodies are rejected.
func DecodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		switch {
		case errors.As(err, &maxErr):
			return ErrPayloadTooLarge("request body too large")
		case errors.Is(err, io.EOF):
			return ErrInvalidInput("request body is empty")
		default:
			return ErrInvalidInput("invalid JSON body")
		}
	}
	if dec.More() {
		return ErrInvalidInput("request body must contain a single JSON object")
	}
	return nil
}

// PathUUID parses a UUID path parameter.
func PathUUID(r *http.Request, name string) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		return uuid.Nil, ErrInvalidInput("invalid " + name)
	}
	return id, nil
}

// QueryLimit reads an integer query parameter clamped to [1, max]; a missing
// or malformed value yields def.
func QueryLimit(r *http.Request, name string, def, max int) int {
	v, err := strconv.Atoi(r.URL.Query().Get(name))
	if err != nil || v < 1 {
		return def
	}
	if v > max {
		return max
	}
	return v
}

// CleanText trims s and checks it is valid UTF-8 without NUL bytes and at most
// maxRunes characters. field names the value in error messages.
func CleanText(field, s string, maxRunes int, required bool) (string, error) {
	s = strings.TrimSpace(s)
	if required && s == "" {
		return "", ErrInvalidInput(field + " is required")
	}
	if !utf8.ValidString(s) || strings.ContainsRune(s, 0) {
		return "", ErrInvalidInput(field + " contains invalid characters")
	}
	if utf8.RuneCountInString(s) > maxRunes {
		return "", ErrInvalidInput(fmt.Sprintf("%s must be at most %d characters", field, maxRunes))
	}
	return s, nil
}

// CleanName is CleanText for display names that others read as an identity
// (webhook, bot and username overrides): invisible and bidirectional format
// characters (Unicode Cf, such as U+202E and U+200B), private-use characters
// and C1 controls are removed first, so a name cannot be spoofed with text
// that renders differently from what it spells. Only the joiners that a script
// or an emoji sequence really needs survive (see stripInvisible).
func CleanName(field, s string, maxRunes int, required bool) (string, error) {
	if utf8.ValidString(s) {
		s = stripInvisible(s)
	}
	return CleanText(field, s, maxRunes, required)
}

// isInvisibleRune reports format, private-use and C1 control characters.
func isInvisibleRune(r rune) bool {
	return unicode.In(r, unicode.Cf, unicode.Co) || (r >= 0x80 && r <= 0x9f) || r == 0x2028 || r == 0x2029
}

// SanitizeFilename strips path components, control characters and leading
// dots so the name is safe as a Content-Disposition base name. It returns ""
// when nothing usable remains. Adopted from mnema.xyz.
func SanitizeFilename(name string, maxLen int) string {
	name = stripPathComponents(name)
	name = filterFilenameRunes(name)
	name = strings.TrimSpace(strings.TrimLeft(name, "."))
	if name == "" || !utf8.ValidString(name) {
		return ""
	}
	return truncateRuneBoundary(name, maxLen)
}

func stripPathComponents(name string) string {
	name = strings.TrimSpace(name)
	if i := strings.LastIndexAny(name, "/\\"); i >= 0 {
		return name[i+1:]
	}
	return name
}

func isForbiddenFilenameRune(r rune) bool {
	return r < 0x20 || r == 0x7f || r == '"' || isInvisibleRune(r)
}

func filterFilenameRunes(name string) string {
	return strings.Map(func(r rune) rune {
		if isForbiddenFilenameRune(r) {
			return -1
		}
		return r
	}, name)
}

func truncateRuneBoundary(name string, maxLen int) string {
	if maxLen <= 0 || len(name) <= maxLen {
		return name
	}
	// Cut on a rune boundary so the result stays valid UTF-8.
	cut := maxLen
	for cut > 0 && !utf8.RuneStart(name[cut]) {
		cut--
	}
	return name[:cut]
}
