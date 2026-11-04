package linkpreview

import (
	"testing"
)

func TestCheckURLSharesSSRFShape(t *testing.T) {
	f := New()
	for _, raw := range []string{"https://example.com/a b", "https://example.com/\x00", "mailto:a@example.com"} {
		if _, err := f.parse(raw); err == nil {
			t.Errorf("%q must be refused", raw)
		}
	}
}
