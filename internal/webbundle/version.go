package webbundle

import (
	"fmt"
	"strconv"
	"strings"
)

// Version is a release version, MAJOR.MINOR.PATCH.
type Version [3]uint32

func isValidVersionPart(p string) bool {
	if p == "" || len(p) > 9 {
		return false
	}
	return len(p) == 1 || p[0] != '0'
}

// ParseVersion reads X.Y.Z (no "v" prefix, no pre-release or build suffix).
func ParseVersion(s string) (Version, error) {
	var v Version
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return v, fmt.Errorf("%q is not MAJOR.MINOR.PATCH", s)
	}
	for i, p := range parts {
		if !isValidVersionPart(p) {
			return v, fmt.Errorf("%q is not MAJOR.MINOR.PATCH", s)
		}
		n, err := strconv.ParseUint(p, 10, 32)
		if err != nil {
			return v, fmt.Errorf("%q is not MAJOR.MINOR.PATCH", s)
		}
		v[i] = uint32(n)
	}
	return v, nil
}

// CompareVersions returns -1, 0 or 1.
func CompareVersions(a, b string) (int, error) {
	va, err := ParseVersion(a)
	if err != nil {
		return 0, err
	}
	vb, err := ParseVersion(b)
	if err != nil {
		return 0, err
	}
	for i := range va {
		switch {
		case va[i] < vb[i]:
			return -1, nil
		case va[i] > vb[i]:
			return 1, nil
		}
	}
	return 0, nil
}
