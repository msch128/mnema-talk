// Package update finds out whether a newer Mnema Talk release exists (GitHub
// releases API, polled by Checker) and, when the operator opted in, asks the
// isolated updater sidecar to pull it (Updater). The app itself never talks
// to Docker.
package update

import (
	"regexp"
	"strconv"
	"strings"
)

// Semver is a parsed release version MAJOR.MINOR.PATCH[-PRERELEASE].
type Semver struct {
	Major, Minor, Patch int
	Pre                 string
}

var semverPattern = regexp.MustCompile(`^v?(\d{1,6})\.(\d{1,6})\.(\d{1,6})(?:-([0-9A-Za-z.-]{1,64}))?$`)

// ParseSemver parses "1.2.3", "v1.2.3" or "1.2.3-rc.1". Anything else,
// including "dev", is rejected.
func ParseSemver(s string) (Semver, bool) {
	m := semverPattern.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return Semver{}, false
	}
	var v Semver
	v.Major, _ = strconv.Atoi(m[1])
	v.Minor, _ = strconv.Atoi(m[2])
	v.Patch, _ = strconv.Atoi(m[3])
	v.Pre = m[4]
	return v, true
}

// String formats v without a leading "v".
func (v Semver) String() string {
	s := strconv.Itoa(v.Major) + "." + strconv.Itoa(v.Minor) + "." + strconv.Itoa(v.Patch)
	if v.Pre != "" {
		s += "-" + v.Pre
	}
	return s
}

// Compare returns -1, 0 or 1. A pre-release sorts before its release;
// pre-release identifiers compare as plain strings (good enough for rc.N).
func (v Semver) Compare(o Semver) int {
	for _, d := range [3]int{v.Major - o.Major, v.Minor - o.Minor, v.Patch - o.Patch} {
		if d < 0 {
			return -1
		}
		if d > 0 {
			return 1
		}
	}
	switch {
	case v.Pre == o.Pre:
		return 0
	case v.Pre == "":
		return 1
	case o.Pre == "":
		return -1
	case v.Pre < o.Pre:
		return -1
	default:
		return 1
	}
}

// IsNewer reports whether latest is a strictly newer release than current.
// Unparsable versions (a "dev" build) never count as outdated.
func IsNewer(latest, current string) bool {
	l, ok1 := ParseSemver(latest)
	c, ok2 := ParseSemver(current)
	return ok1 && ok2 && l.Compare(c) > 0
}
