package communityevents

import (
	"testing"
	"time"
)

func mustZone(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Skipf("no time zone data for %s: %v", name, err)
	}
	return loc
}

func TestOccurrencesKeepTheirWallClockTimeAcrossSummerTime(t *testing.T) {
	berlin := mustZone(t, "Europe/Berlin")
	anchor := time.Date(2026, 10, 19, 19, 0, 0, 0, berlin) // Monday, summer time (UTC+2)
	for n, want := range []struct{ y, m, d int }{{2026, 10, 19}, {2026, 10, 26}, {2026, 11, 2}} {
		got := OccurrenceStart(anchor, berlin, FreqWeekly, n).In(berlin)
		if got.Year() != want.y || int(got.Month()) != want.m || got.Day() != want.d || got.Hour() != 19 || got.Minute() != 0 {
			t.Fatalf("n=%d: %v, want %v at 19:00 local", n, got, want)
		}
	}
	// The clocks went back on 25 October: the UTC time moved by an hour, the local time did not.
	before := OccurrenceStart(anchor, berlin, FreqWeekly, 0)
	after := OccurrenceStart(anchor, berlin, FreqWeekly, 1)
	if after.Sub(before) != 7*24*time.Hour+time.Hour {
		t.Fatalf("the week across the change is an hour longer, got %v", after.Sub(before))
	}
}

func TestBiweeklyAndMonthlyStepsAreCountedFromTheAnchor(t *testing.T) {
	utc := time.UTC
	anchor := time.Date(2026, 1, 31, 8, 30, 0, 0, utc)
	if got := OccurrenceStart(anchor, utc, FreqBiweekly, 3); !got.Equal(time.Date(2026, 3, 14, 8, 30, 0, 0, utc)) {
		t.Fatalf("biweekly: %v", got)
	}
	for n, want := range []time.Time{
		time.Date(2026, 1, 31, 8, 30, 0, 0, utc), time.Date(2026, 2, 28, 8, 30, 0, 0, utc), time.Date(2026, 3, 31, 8, 30, 0, 0, utc),
		time.Date(2026, 4, 30, 8, 30, 0, 0, utc), time.Date(2026, 5, 31, 8, 30, 0, 0, utc), time.Date(2027, 1, 31, 8, 30, 0, 0, utc),
	} {
		step := n
		if n == 5 {
			step = 12
		}
		if got := OccurrenceStart(anchor, utc, FreqMonthly, step); !got.Equal(want) {
			t.Errorf("monthly n=%d: %v, want %v (a short month must not shift the months after it)", step, got, want)
		}
	}
	if got := OccurrenceStart(time.Date(2028, 1, 31, 0, 0, 0, 0, utc), utc, FreqMonthly, 1); got.Day() != 29 {
		t.Fatalf("a leap year keeps the 29th: %v", got)
	}
}

func TestZonesAndFrequenciesAreChecked(t *testing.T) {
	if loc, err := LoadZone(""); err != nil || loc != time.UTC {
		t.Fatalf("empty is UTC: %v %v", loc, err)
	}
	for _, bad := range []string{"Mars/Olympus", "Local"} {
		if _, err := LoadZone(bad); err == nil {
			t.Fatalf("%q must be refused", bad)
		}
	}
	for f, ok := range map[string]bool{FreqWeekly: true, FreqBiweekly: true, FreqMonthly: true, "daily": false, "": false} {
		if ValidFrequency(f) != ok {
			t.Errorf("frequency %q", f)
		}
	}
}

func TestAnOccurrenceInTheSpringForwardGapNeitherVanishesNorMovesTheNextOnes(t *testing.T) {
	berlin := mustZone(t, "Europe/Berlin")
	anchor := time.Date(2026, 3, 22, 2, 30, 0, 0, berlin) // the clocks go 02:00 to 03:00 on 29 March
	gap := OccurrenceStart(anchor, berlin, FreqWeekly, 1).In(berlin)
	if gap.Day() != 29 || gap.Month() != time.March || gap.Hour() < 3 {
		t.Fatalf("02:30 does not exist on 29 March; the occurrence lands after the gap on the same day, got %v", gap)
	}
	if next := OccurrenceStart(anchor, berlin, FreqWeekly, 2).In(berlin); next.Day() != 5 || next.Hour() != 2 || next.Minute() != 30 {
		t.Fatalf("the week after the gap is back at 02:30: %v", next)
	}
}

func TestMonthlyStepsKeepTheWallClockAcrossSummerTime(t *testing.T) {
	berlin := mustZone(t, "Europe/Berlin")
	anchor := time.Date(2026, 3, 15, 9, 0, 0, 0, berlin) // winter time
	april := OccurrenceStart(anchor, berlin, FreqMonthly, 1)
	local := april.In(berlin)
	if local.Hour() != 9 || local.Day() != 15 {
		t.Fatalf("still 09:00 on the 15th: %v", local)
	}
	_, winter := anchor.Zone()
	_, summer := local.Zone()
	if summer-winter != 3600 {
		t.Fatalf("the offset moved by an hour (%d to %d), the wall clock did not", winter, summer)
	}
}
