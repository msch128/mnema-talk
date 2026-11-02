// Package presets loads the ready-made choices of the web client's quick menus
// (web/src/lib/presets.json), so a server test can push every one of them
// through the real validation. The client imports the same file.
package presets

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Set is the content of presets.json.
type Set struct {
	SlowModeSeconds         []int    `json:"slowModeSeconds"`
	InviteCreateHours       []int    `json:"inviteCreateHours"`
	InviteExpiryMinutes     []int    `json:"inviteExpiryMinutes"`
	InviteUses              []int    `json:"inviteUses"`
	ModerationSeconds       []int    `json:"moderationSeconds"`
	PollDurations           []string `json:"pollDurations"`
	LockdownHours           []int    `json:"lockdownHours"`
	NewMemberWaitMinutes    []int    `json:"newMemberWaitMinutes"`
	MuteMinutes             []int    `json:"muteMinutes"`
	AutomodAccountAges      []int    `json:"automodAccountAges"`
	AutomodRaidWindows      []int    `json:"automodRaidWindows"`
	AutomodRaidRestrictions []int    `json:"automodRaidRestrictions"`
	AutomodRuleTimeouts     []int    `json:"automodRuleTimeouts"`
}

// Load reads the file next to the web client's code and fails the test when
// it is missing, unknown fields are present or a list is empty.
func Load(t testing.TB) Set {
	t.Helper()
	_, here, _, _ := runtime.Caller(0)
	path := filepath.Join(filepath.Dir(here), "..", "..", "..", "web", "src", "lib", "presets.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read presets: %v", err)
	}
	var s Set
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&s); err != nil {
		t.Fatalf("decode presets: %v", err)
	}
	for name, n := range map[string]int{
		"slowModeSeconds": len(s.SlowModeSeconds), "inviteCreateHours": len(s.InviteCreateHours),
		"inviteExpiryMinutes": len(s.InviteExpiryMinutes), "inviteUses": len(s.InviteUses),
		"moderationSeconds": len(s.ModerationSeconds), "pollDurations": len(s.PollDurations),
		"muteMinutes": len(s.MuteMinutes), "lockdownHours": len(s.LockdownHours), "newMemberWaitMinutes": len(s.NewMemberWaitMinutes), "automodAccountAges": len(s.AutomodAccountAges),
		"automodRaidWindows": len(s.AutomodRaidWindows), "automodRaidRestrictions": len(s.AutomodRaidRestrictions),
		"automodRuleTimeouts": len(s.AutomodRuleTimeouts),
	} {
		if n == 0 {
			t.Fatalf("presets.json: %s is empty", name)
		}
	}
	return s
}
