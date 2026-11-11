package imgstrip

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the fixture files under testdata")

// The phone fixtures are files too, so the upload tests of other packages
// and a person with an EXIF viewer can use them. They hold invented data
// only. This keeps them equal to their builders; -update rewrites them.
func TestFixtureFilesMatchTheirBuilders(t *testing.T) {
	files := map[string][]byte{
		"phone-orientation-6.jpg":  phoneJPEG(t, 6),
		"phone-orientation-6.png":  phonePNG(t, 6),
		"phone-orientation-6.webp": phoneWebP(6),
	}
	for name, want := range files {
		path := filepath.Join("testdata", name)
		if *update {
			if err := os.WriteFile(path, want, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if got, err := os.ReadFile(path); err != nil || !bytes.Equal(got, want) {
			t.Errorf("%s is not what its builder makes (run go test ./internal/imgstrip -update): %v", path, err)
		}
	}
}
