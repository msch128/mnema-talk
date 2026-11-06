package discordimport

import (
	"slices"
	"strings"
	"testing"

	"github.com/msch128/mnema-talk/internal/perm"
)

func TestEveryPlanHintCarriesAKnownCodeAndKeepsItsEnglishText(t *testing.T) {
	tpl := parseSample(t)
	long := strings.Repeat("t", 2000)
	tpl.Guild.Channels = append(tpl.Guild.Channels, SrcChan{ID: "77", Type: typeText, Name: "wordy", Topic: &long})
	for _, caps := range []Capabilities{{}, {Forum: true, Stage: true}} {
		p := BuildPlan(tpl, Inputs{Grantable: grantable(perm.All), Caps: caps})
		seen := map[string]bool{}
		for _, c := range p.Channels {
			if len(c.NoteHints) != len(c.Notes) {
				t.Fatalf("%s: %d hints for %d notes", c.Name, len(c.NoteHints), len(c.Notes))
			}
			for _, h := range c.NoteHints {
				if !slices.Contains(NoteCodes, h.Code) || h.Params == nil {
					t.Fatalf("%s: unknown note code %q", c.Name, h.Code)
				}
				seen[h.Code] = true
			}
		}
		if len(p.SkippedHints) != len(p.Skipped) || len(p.Skipped) == 0 {
			t.Fatalf("skipped %v hints %v", p.Skipped, p.SkippedHints)
		}
		for _, h := range p.SkippedHints {
			if !slices.Contains(SkipCodes, h.Code) || h.Params["name"] == "" {
				t.Fatalf("bad skip hint %+v", h)
			}
		}
		for _, code := range []string{NoteTopicShortened, NoteSlowModeCapped, NoteAgeRestriction, NoteAnnouncementToText} {
			if !seen[code] {
				t.Errorf("caps %+v: code %s never emitted", caps, code)
			}
		}
	}
}
