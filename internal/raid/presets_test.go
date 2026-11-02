package raid

import (
	"testing"

	"github.com/msch128/mnema-talk/internal/testutil/presets"
)

// Every choice of the raid protection menus passes the validation the routes run.
func TestClientPresetsPassValidation(t *testing.T) {
	set := presets.Load(t)
	for _, h := range set.LockdownHours {
		if err := (LockdownInput{Active: true, Hours: h}).Validate(); err != nil {
			t.Errorf("lockdown of %d h: %v", h, err)
		}
	}
	for _, m := range set.NewMemberWaitMinutes {
		in := Input{JoinWindowMinutes: 10, MinAgeWriteMinutes: m, MinAgeReactMinutes: m, MinAgeSpeakMinutes: m}
		if err := in.Validate(); err != nil {
			t.Errorf("wait of %d min: %v", m, err)
		}
	}
}

func TestValidationNamesTheBound(t *testing.T) {
	good := Input{JoinLimit: 5, JoinWindowMinutes: 10}
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	for name, in := range map[string]Input{
		"limit":  {JoinLimit: MaxJoinLimit + 1, JoinWindowMinutes: 10},
		"window": {JoinWindowMinutes: 0},
		"write":  {JoinWindowMinutes: 10, MinAgeWriteMinutes: MaxMinAgeMinutes + 1},
		"react":  {JoinWindowMinutes: 10, MinAgeReactMinutes: -1},
		"speak":  {JoinWindowMinutes: 10, MinAgeSpeakMinutes: MaxMinAgeMinutes + 1},
	} {
		if err := in.Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if err := (LockdownInput{Hours: MaxLockdownHours + 1}).Validate(); err == nil {
		t.Error("lockdown hours: accepted")
	}
}
