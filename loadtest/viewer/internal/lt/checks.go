package lt

import "fmt"

// Checks collects the thresholds a run violated.
type Checks struct {
	Violations []string
}

// Max records a violation when got is above limit. A limit of zero or less
// switches the check off, so "0" is spelled as a tiny positive number where a
// zero tolerance is meant; use Zero for that.
func (c *Checks) Max(what string, got, limit float64) {
	if limit > 0 && got > limit {
		c.Violations = append(c.Violations, fmt.Sprintf("%s %.4g is above the limit %.4g", what, got, limit))
	}
}

// Zero records a violation when got is above the allowed count (which may be
// zero).
func (c *Checks) Zero(what string, got, allowed int64) {
	if got > allowed {
		c.Violations = append(c.Violations, fmt.Sprintf("%s %d is above the allowed %d", what, got, allowed))
	}
}

// Failf records a violation with a free text.
func (c *Checks) Failf(format string, args ...any) {
	c.Violations = append(c.Violations, fmt.Sprintf(format, args...))
}

// OK reports whether no threshold was violated.
func (c *Checks) OK() bool { return len(c.Violations) == 0 }
