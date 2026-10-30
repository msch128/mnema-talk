package onboarding

import (
	"encoding/json"
	"testing"
)

func TestBuildFirstRunTicksStepsFromTheFacts(t *testing.T) {
	cases := []struct {
		name     string
		facts    firstRunFacts
		stored   storedFirstRun
		done     []string
		hidden   bool
		complete bool
	}{
		{"nothing yet", firstRunFacts{}, storedFirstRun{}, nil, false, false},
		{"facts tick their steps", firstRunFacts{named: true, invited: true, lobby: true, rules: true}, storedFirstRun{}, []string{StepName, StepInvite, StepVoice, StepRolesRules}, false, false},
		{"visits tick the page steps", firstRunFacts{}, storedFirstRun{Visited: []string{StepChannels, StepStatus}}, []string{StepChannels, StepStatus}, false, false},
		{"complete without the optional step", firstRunFacts{named: true, invited: true, lobby: true}, storedFirstRun{Visited: []string{StepChannels, StepStatus}}, []string{StepName, StepInvite, StepChannels, StepVoice, StepStatus}, false, true},
		{"an established community starts closed", firstRunFacts{established: true, invited: true}, storedFirstRun{}, []string{StepInvite}, true, false},
		{"the Owner's choice beats the default", firstRunFacts{established: true}, storedFirstRun{Hidden: ptr(false)}, nil, false, false},
		{"closed stays closed", firstRunFacts{}, storedFirstRun{Hidden: ptr(true)}, nil, true, false},
	}
	for _, tc := range cases {
		got := buildFirstRun(tc.facts, tc.stored)
		if len(got.Steps) != len(firstRunSteps) {
			t.Fatalf("%s: %d steps", tc.name, len(got.Steps))
		}
		done := map[string]bool{}
		for _, id := range tc.done {
			done[id] = true
		}
		for _, s := range got.Steps {
			if s.Done != done[s.ID] {
				t.Errorf("%s: step %s done=%v", tc.name, s.ID, s.Done)
			}
		}
		if got.Hidden != tc.hidden || got.Complete != tc.complete {
			t.Errorf("%s: hidden=%v complete=%v, want %v %v", tc.name, got.Hidden, got.Complete, tc.hidden, tc.complete)
		}
	}
}

func ptr(b bool) *bool { return &b }

func TestFirstRunRecordKeepsTheRestOfTheOnboardingJSON(t *testing.T) {
	raw := []byte(`{"dismissed":["search"],"welcome_done":true,"future":{"a":1}}`)
	if s := readFirstRun(raw); s.Hidden != nil || len(s.Visited) != 0 {
		t.Fatalf("empty record read as %+v", s)
	}
	merged, err := withFirstRun(raw, storedFirstRun{Hidden: ptr(true), Visited: []string{StepStatus}})
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(merged, &doc); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"dismissed", "welcome_done", "future", firstRunKey} {
		if _, ok := doc[key]; !ok {
			t.Errorf("key %q lost: %s", key, merged)
		}
	}
	got := readFirstRun(merged)
	if got.Hidden == nil || !*got.Hidden || len(got.Visited) != 1 || got.Visited[0] != StepStatus {
		t.Errorf("round trip: %+v", got)
	}
	// Tips are stored through the same JSON; they must leave the first-run record alone.
	again, err := mergeTips(merged, TipsInput{DismissedTips: []string{"mention"}})
	if err != nil {
		t.Fatal(err)
	}
	if after := readFirstRun(again); after.Hidden == nil || !*after.Hidden {
		t.Errorf("a tip dismissal dropped the first-run record: %s", again)
	}
}
