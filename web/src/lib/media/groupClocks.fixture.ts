// The clock plans the sweep of groupClocks.sweep.test.ts runs. Each is a way real machines differ:
// - offset: wall and monotonic clocks hours apart (a wrong timezone, a drifted laptop);
// - drift: monotonic clocks that run 0.5 % fast or slow (a hundred times what a good oscillator does);
// - steps: wall clocks set by up to 10 minutes while the Talk runs (NTP after a long sleep, a VM resumed),
//   and monotonic clocks that stand still while the machine sleeps (macOS and Linux do);
// - all: everything at once.
import type { ClockPlan } from './groupSimRun.fixture'

export const CLOCK_VARIANTS: Readonly<Record<string, ClockPlan>> = {
  none: { offsetMs: 0, driftPpm: 0, stepMs: 0, monoStops: 0 },
  offset: { offsetMs: 3_600_000, driftPpm: 0, stepMs: 0, monoStops: 0 },
  drift: { offsetMs: 0, driftPpm: 5000, stepMs: 0, monoStops: 0 },
  steps: { offsetMs: 0, driftPpm: 0, stepMs: 600_000, monoStops: 0.5 },
  all: { offsetMs: 3_600_000, driftPpm: 5000, stepMs: 600_000, monoStops: 0.5 },
}
