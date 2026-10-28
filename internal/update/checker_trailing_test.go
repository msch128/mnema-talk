package update

import "testing"

func TestParseReleaseRejectsStrayClosingBrace(t *testing.T) {
	if _, err := parseRelease([]byte(`{"tag_name":"v1.0.0"}}`)); err == nil {
		t.Fatal("stray closing brace accepted")
	}
}
