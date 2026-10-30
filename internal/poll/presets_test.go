package poll

import (
	"slices"
	"testing"

	"github.com/msch128/mnema-talk/internal/testutil/presets"
)

// Every duration of the poll dialog is accepted by the poll validation, and
// the dialog offers exactly the durations the server lists.
func TestClientPollDurationsPassValidation(t *testing.T) {
	names := presets.Load(t).PollDurations
	for _, d := range names {
		in := Input{Question: "Lunch?", Options: []string{"Pizza", "Pasta"}, Duration: d}
		if _, err := in.clean(); err != nil {
			t.Errorf("duration %q: %v", d, err)
		}
	}
	if !slices.Equal(names, DurationNames()) {
		t.Errorf("dialog offers %v, server lists %v", names, DurationNames())
	}
}
