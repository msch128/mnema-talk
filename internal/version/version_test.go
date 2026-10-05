package version

import "testing"

func TestCurrent(t *testing.T) {
	defer func(v string) { Version = v }(Version)
	for in, want := range map[string]string{
		"":              Dev,
		"dev":           Dev,
		"0.4.0":         "0.4.0",
		"v1.2.3":        "1.2.3",
		" 1.2.3-rc.1 ":  "1.2.3-rc.1",
		"1.2.3<script>": Dev,
		"1.2\n3":        Dev,
	} {
		Version = in
		if got := Current(); got != want {
			t.Errorf("Current() with %q = %q, want %q", in, got, want)
		}
	}
}

func TestCommit(t *testing.T) {
	defer func(v string) { Revision = v }(Revision)
	for in, want := range map[string]string{
		"":        "",
		"unknown": "",
		"abc123":  "abc123",
		"0123456789abcdef0123456789abcdef01234567": "0123456789ab",
		"abc def": "",
	} {
		Revision = in
		if got := Commit(); got != want {
			t.Errorf("Commit() with %q = %q, want %q", in, got, want)
		}
	}
}
