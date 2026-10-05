// Package version holds the build's release version and source revision.
// Both are set at link time:
//
//	go build -ldflags "-X github.com/msch128/mnema-talk/internal/version.Version=1.2.3 \
//	                   -X github.com/msch128/mnema-talk/internal/version.Revision=<git sha>"
//
// The Dockerfile, the Makefile and the release workflow pass them; a plain
// `go build` / `go run` reports "dev". The web app embeds the same version
// (vite define __APP_VERSION__), so a browser can tell when the server it
// talks to runs a newer build than the page it loaded.
package version

import (
	"regexp"
	"runtime"
	"strings"
)

// Dev is the version of a build without release metadata.
const Dev = "dev"

var (
	// Version is the release version without a leading "v", e.g. "0.4.0".
	Version = Dev
	// Revision is the git commit the binary was built from (may be empty).
	Revision = ""
)

// safe limits what a link-time value may contain, so a stray build argument
// can never smuggle markup or control characters into API responses.
var safe = regexp.MustCompile(`^[0-9A-Za-z.+_-]{1,64}$`)

// Current returns the build's version, or "dev" when none (or an invalid
// one) was linked in. A leading "v" is dropped.
func Current() string {
	v := strings.TrimPrefix(strings.TrimSpace(Version), "v")
	if !safe.MatchString(v) {
		return Dev
	}
	return v
}

// Commit returns the build's source revision, shortened to 12 characters,
// or "" when unknown.
func Commit() string {
	r := strings.TrimSpace(Revision)
	if !safe.MatchString(r) || r == "unknown" {
		return ""
	}
	if len(r) > 12 {
		r = r[:12]
	}
	return r
}

// GoVersion is the Go toolchain the binary was built with.
func GoVersion() string { return runtime.Version() }
