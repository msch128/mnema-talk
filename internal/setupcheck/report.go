package setupcheck

import (
	"fmt"
	"io"
)

// Report prints the checks as text for a terminal: one line per check with its
// state, the fix underneath each amber or red one, and a summary. It returns
// the worst level so a caller can turn red into a non-zero exit code.
func Report(w io.Writer, checks []Check) Level {
	marks := map[Level]string{OK: "[ ok ]", Info: "[note]", Warn: "[warn]", Fail: "[FAIL]"}
	attention := 0
	for _, c := range checks {
		fmt.Fprintf(w, "%s %s\n", marks[c.Level], c.Detail)
		if c.Level == Warn || c.Level == Fail {
			attention++
		}
		if c.Fix != "" && c.Level != OK {
			fmt.Fprintf(w, "       fix: %s\n", c.Fix)
		}
	}
	worst := Worst(checks)
	fmt.Fprintf(w, "\n%d of %d checks ok", len(checks)-attention, len(checks))
	if attention > 0 {
		fmt.Fprintf(w, ", %d need attention", attention)
	}
	fmt.Fprintln(w, ".")
	return worst
}
