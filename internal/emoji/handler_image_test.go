package emoji

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteImageRevalidatesWithWeakAndListedValidators(t *testing.T) {
	hash := strings.Repeat("ab", 32)
	etag := `"` + hash[:16] + `"`
	for name, inm := range map[string]string{
		"strong": etag,
		"weak":   "W/" + etag,
		"list":   `"other", ` + etag,
		"star":   "*",
	} {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("If-None-Match", inm)
		w := httptest.NewRecorder()
		// A nil store proves the 304 path never reads the object.
		if err := (&Handler{}).writeImage(w, r, "key", TypePNG, hash, false); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if w.Code != http.StatusNotModified {
			t.Errorf("%s: status = %d, want 304", name, w.Code)
		}
	}
	for _, inm := range []string{`"other"`, `W/"other"`, ""} {
		if etagMatches([]string{inm}, etag) {
			t.Errorf("If-None-Match %q must not match", inm)
		}
	}
}
