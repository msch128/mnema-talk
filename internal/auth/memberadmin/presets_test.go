package memberadmin

import (
	"fmt"
	"testing"

	"github.com/msch128/mnema-talk/internal/testutil/presets"
)

// Every ready-made choice of the web client's moderation menus has to pass the
// validation the routes run, so none can fail silently.
func TestClientPresetsPassModerationValidation(t *testing.T) {
	for _, s := range presets.Load(t).ModerationSeconds {
		t.Run(fmt.Sprint(s), func(t *testing.T) {
			if err := validateTimeoutSeconds(s); err != nil {
				t.Errorf("timeout: %v", err)
			}
			if _, err := parseBanDuration(&s); err != nil {
				t.Errorf("ban: %v", err)
			}
		})
	}
}
