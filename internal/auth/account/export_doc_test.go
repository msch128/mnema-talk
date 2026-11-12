package account

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// SECURITY.md states the bounds of the data export as numbers; this pins them
// to the variables. Finding H-L3-07 of the 2026-11 hunt: the sentence says
// 64 MiB for the messages, which is the bound of the compressed archive
// (exportMaxBytes, which fails the export); the messages stop at
// exportMessageBytes. knownExportBoundMismatch asserts the WRONG state (an
// inverse assertion) and fails the day the sentence matches the code.
func TestSecurityMdStatesTheExportBoundsOfTheCode(t *testing.T) {
	raw, err := os.ReadFile("../../../SECURITY.md")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Join(strings.Fields(string(raw)), " ")
	m := regexp.MustCompile(`bounded to ([\d ]+) messages and (\d+) MiB`).FindStringSubmatch(text)
	if m == nil {
		t.Fatal(`SECURITY.md no longer says "bounded to N messages and M MiB": update this test`)
	}
	if n, _ := strconv.Atoi(strings.ReplaceAll(m[1], " ", "")); n != exportMaxMessages {
		t.Errorf("SECURITY.md says %d messages, the code stops at %d", n, exportMaxMessages)
	}
	knownExportBoundMismatch(t, m[2])
}

func knownExportBoundMismatch(t *testing.T, statedMiB string) {
	t.Helper()
	mib, _ := strconv.Atoi(statedMiB)
	if mib == exportMessageBytes>>20 {
		t.Errorf("SECURITY.md now states the message bound of the code (%d MiB): H-L3-07 is fixed for it, make this a plain check", mib)
	}
	if int64(mib) != exportMaxBytes>>20 {
		t.Errorf("SECURITY.md states %d MiB, neither the message bound (%d) nor the archive bound (%d)", mib, exportMessageBytes>>20, exportMaxBytes>>20)
	}
}
