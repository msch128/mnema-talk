package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
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

// SanitizeFilename strips path components, control characters and leading
// dots so the name is safe as a Content-Disposition base name. It returns ""
// when nothing usable remains. Adopted from mnema.xyz.
func SanitizeFilename(name string, maxLen int) string {
	name = strings.TrimSpace(name)
	if i := strings.LastIndexAny(name, "/\\"); i >= 0 {
		name = name[i+1:]
	}
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || r == '"' {
			return -1
		}
		return r
	}, name)
	name = strings.TrimSpace(strings.TrimLeft(name, "."))
	if name == "" || !utf8.ValidString(name) {
		return ""
	}
	if maxLen > 0 && len(name) > maxLen {
		// Cut on a rune boundary so the result stays valid UTF-8.
		cut := maxLen
		for cut > 0 && !utf8.RuneStart(name[cut]) {
			cut--
		}
		name = name[:cut]
	}
	return name
}
