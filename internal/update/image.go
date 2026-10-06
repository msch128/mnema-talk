package update

import (
	"regexp"
	"strconv"
	"strings"
)

// Reach says whether pulling the configured image again can bring the app to
// the latest release. The updater sidecar re-pulls the tag the container
// runs; it never switches tags.
type Reach string

const (
	// ReachYes: the tag moves with releases that include the latest one
	// (":latest", ":0.4" for 0.4.x, ":0" for 0.x.y).
	ReachYes Reach = "yes"
	// ReachNo: a pinned version or digest, a locally built image, or a
	// moving tag that does not cover the latest release (":0.3" vs 0.4.0).
	ReachNo Reach = "no"
	// ReachUnknown: MNEMA_IMAGE is not set or uses a tag we can't interpret.
	ReachUnknown Reach = "unknown"
)

// Reasons accompanying Reach (stable strings; the web app translates them).
const (
	ReasonFollows       = "follows"
	ReasonUnset         = "unset"
	ReasonLocal         = "local_build"
	ReasonDigest        = "digest_pinned"
	ReasonPinned        = "version_pinned"
	ReasonTrackMismatch = "track_mismatch"
	ReasonCustomTag     = "custom_tag"
)

var (
	majorTag      = regexp.MustCompile(`^v?(\d{1,6})$`)
	majorMinorTag = regexp.MustCompile(`^v?(\d{1,6})\.(\d{1,6})$`)
)

// SplitImage splits an image reference into repository and tag (default
// "latest"); digest is set for "repo@sha256:..." references.
func SplitImage(ref string) (repo, tag, digest string) {
	ref = strings.TrimSpace(ref)
	if at := strings.Index(ref, "@"); at >= 0 {
		ref, digest = ref[:at], ref[at+1:]
	}
	repo, tag = ref, ""
	if colon := strings.LastIndex(ref, ":"); colon > strings.LastIndex(ref, "/") {
		repo, tag = ref[:colon], ref[colon+1:]
	}
	if tag == "" {
		tag = "latest"
	}
	return repo, tag, digest
}

// ImageReach decides whether re-pulling image (MNEMA_IMAGE) can reach the
// release latest.
func ImageReach(image, latest string) (Reach, string) {
	if strings.TrimSpace(image) == "" {
		return ReachUnknown, ReasonUnset
	}
	repo, tag, digest := SplitImage(image)
	if digest != "" {
		return ReachNo, ReasonDigest
	}
	// "mnema-talk:local" and other names without a registry/namespace are
	// images built on the host; there is nothing to pull.
	if !strings.Contains(repo, "/") {
		return ReachNo, ReasonLocal
	}
	if tag == "latest" {
		return ReachYes, ReasonFollows
	}
	l, ok := ParseSemver(latest)
	if !ok {
		return ReachUnknown, ReasonCustomTag
	}
	if m := majorTag.FindStringSubmatch(tag); m != nil {
		if atoi(m[1]) == l.Major {
			return ReachYes, ReasonFollows
		}
		return ReachNo, ReasonTrackMismatch
	}
	if m := majorMinorTag.FindStringSubmatch(tag); m != nil {
		if atoi(m[1]) == l.Major && atoi(m[2]) == l.Minor {
			return ReachYes, ReasonFollows
		}
		return ReachNo, ReasonTrackMismatch
	}
	if _, ok := ParseSemver(tag); ok {
		return ReachNo, ReasonPinned
	}
	return ReachUnknown, ReasonCustomTag
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
