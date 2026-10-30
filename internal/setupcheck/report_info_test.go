package setupcheck

import (
	"bytes"
	"strings"
	"testing"
)

func TestReportShowsNeutralChecksAsNotesNotAsProblems(t *testing.T) {
	var out bytes.Buffer
	worst := Report(&out, []Check{
		{ID: "a", Level: OK, Detail: "fine"},
		{ID: "b", Level: Info, Detail: "unproven", Fix: "run the test"},
	})
	got := out.String()
	if worst != OK || !strings.Contains(got, "[note] unproven") || !strings.Contains(got, "fix: run the test") || !strings.Contains(got, "2 of 2 checks ok.") || strings.Contains(got, "need attention") {
		t.Fatalf("worst %s, output:\n%s", worst, got)
	}
}
