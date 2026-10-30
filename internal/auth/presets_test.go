package auth

import (
	"fmt"
	"testing"
	"time"

	"github.com/msch128/mnema-talk/internal/testutil/presets"
)

// Every ready-made choice of the web client's invite and moderation menus has
// to pass the validation the routes run, so none can fail silently.
func TestClientPresetsPassInviteValidation(t *testing.T) {
	set := presets.Load(t)
	now := time.Now()
	for _, h := range set.InviteCreateHours {
		if _, err := validateInviteExpiry(&h); err != nil {
			t.Errorf("create-invite validity %d h: %v", h, err)
		}
	}
	for _, m := range set.InviteExpiryMinutes {
		at := now.Add(time.Duration(m) * time.Minute)
		if err := validateNewExpiry(&at, now); err != nil {
			t.Errorf("invite expiry %d min: %v", m, err)
		}
	}
	for _, n := range set.InviteUses {
		if err := validateMaxUses(&n); err != nil {
			t.Errorf("invite uses %d: %v", n, err)
		}
		req := UpdateInviteRequest{MaxUses: &n, setMaxUses: true}
		if err := req.validate(now); err != nil {
			t.Errorf("invite edit uses %d: %v", n, err)
		}
	}
}

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
