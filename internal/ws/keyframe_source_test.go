package ws

import (
	"testing"

	"github.com/msch128/mnema-talk/internal/sfu"
)

// A keyframe request names the sender's own source after it changed (a new
// camera, a new picture size); without a source it is for the videos the
// sender watches.
func TestOwnKeyframeSource(t *testing.T) {
	for _, tc := range []struct {
		payload string
		want    sfu.Source
		own     bool
	}{
		{`{"source":"camera"}`, sfu.SourceCamera, true},
		{`{"source":"screen"}`, sfu.SourceScreen, true},
		{`{}`, "", false},
		{`{"source":"audio"}`, "", false},
		{`not json`, "", false},
		{``, "", false},
	} {
		got, own := ownKeyframeSource([]byte(tc.payload))
		if got != tc.want || own != tc.own {
			t.Errorf("%q: got (%q, %v), want (%q, %v)", tc.payload, got, own, tc.want, tc.own)
		}
	}
}
