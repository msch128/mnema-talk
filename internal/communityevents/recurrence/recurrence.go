// Package recurrence is the repeat rule of an event series: which frequencies
// exist, how a time zone name is read, and when the n-th occurrence starts.
package recurrence

import (
	"time"

	"github.com/msch128/mnema-talk/internal/httpx"
)

const (
	// FreqWeekly, FreqBiweekly and FreqMonthly are the repeat rules of a series.
	FreqWeekly   = "weekly"
	FreqBiweekly = "biweekly"
	FreqMonthly  = "monthly"
)

// ValidFrequency reports whether f is a supported frequency.
func ValidFrequency(f string) bool { return f == FreqWeekly || f == FreqBiweekly || f == FreqMonthly }

// LoadZone reads an IANA time zone name ("" is UTC).
func LoadZone(name string) (*time.Location, error) {
	if name == "" {
		return time.UTC, nil
	}
	if name == "Local" { // the server's own zone is no choice of the member
		return nil, httpx.ErrInvalidInput("timezone must be an IANA time zone name such as Europe/Berlin")
	}
	loc, err := time.LoadLocation(name)
	if err != nil || len(name) > 64 {
		return nil, httpx.ErrInvalidInput("timezone must be an IANA time zone name such as Europe/Berlin")
	}
	return loc, nil
}

// OccurrenceStart is the start of the occurrence n steps after the anchor: the
// anchor's wall-clock time on the day the frequency names, in loc, so summer
// time never moves it. A monthly step keeps the anchor's day of the month and
// falls back to the last day of a shorter month (the 31st becomes the 28th),
// and it is always counted from the anchor, so a short month does not shift the
// months after it.
func OccurrenceStart(anchor time.Time, loc *time.Location, freq string, n int) time.Time {
	a := anchor.In(loc)
	y, m, d := a.Date()
	switch freq {
	case FreqWeekly:
		d += 7 * n
	case FreqBiweekly:
		d += 14 * n
	case FreqMonthly:
		total := int(m) - 1 + n
		y += total / 12
		m = time.Month(total%12 + 1)
		if last := daysIn(y, m); d > last {
			d = last
		}
	}
	return time.Date(y, m, d, a.Hour(), a.Minute(), a.Second(), a.Nanosecond(), loc).UTC()
}

func daysIn(y int, m time.Month) int {
	return time.Date(y, m+1, 0, 0, 0, 0, 0, time.UTC).Day()
}
