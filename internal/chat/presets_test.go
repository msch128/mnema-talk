package chat

import (
	"fmt"
	"testing"

	"github.com/msch128/mnema-talk/internal/testutil/presets"
)

// Every slow mode choice of the channel settings is accepted by the channel edit.
func TestClientSlowModePresetsPassValidation(t *testing.T) {
	for _, s := range presets.Load(t).SlowModeSeconds {
		t.Run(fmt.Sprint(s), func(t *testing.T) {
			if _, _, err := validateChannelEdit(nil, nil, nil, &s); err != nil {
				t.Errorf("slow mode %d s: %v", s, err)
			}
		})
	}
}

// A timed mute of the notification menus stays inside the year the server allows.
func TestClientMutePresetsFitTheServerLimit(t *testing.T) {
	for _, m := range presets.Load(t).MuteMinutes {
		if m <= 0 || m*60 > maxMuteSeconds {
			t.Errorf("mute of %d min is outside 1 s to %d s", m, maxMuteSeconds)
		}
	}
}
