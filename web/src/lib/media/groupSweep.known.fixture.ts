// The failure budget of the deep sweeps of the server powers (seeds 1..200) and of the clocks per member (seeds
// 1..120): the seeds that break an invariant under each power or clock plan, per relay, measured on origin/main 3.0.9
// plus this harness, the fix of H-L2-04 and the fix of H-L2-05 and H-L9-02 (pacer and back-offs on the monotonic clock).
// Like groupSim.known.fixture.ts the lists may only shrink: a listed seed that passes turns the sweep red until it
// leaves the list, a new failing seed turns it red until it is explained. What is left: seed 5 on the hostile relay in
// every plan with steps (two groups of the same members that never merge; it fails as well with the steps left out, so
// the plan's schedule, not the wall clock, causes it), and seed 120 of "all" on the honest relay (fails only with its
// forward step of 447 s, which the sleep check reads as a sleep).
export const KNOWN_POWERS: Readonly<Record<string, readonly number[]>> = {
  'liar/honest': [],
  'liar/hostile': [],
  'replay-commit/honest': [],
  'replay-commit/hostile': [],
  'replay-harmless/honest': [],
  'replay-harmless/hostile': [],
  'replay-key-package/honest': [],
  'replay-key-package/hostile': [],
  'replay/honest': [],
  'replay/hostile': [],
}

export const KNOWN_CLOCKS: Readonly<Record<string, readonly number[]>> = {
  'all/honest': [120],
  'all/hostile': [5],
  'drift/honest': [],
  'drift/hostile': [],
  'extreme/honest': [],
  'extreme/hostile': [],
  'none/honest': [],
  'none/hostile': [],
  'offset/honest': [],
  'offset/hostile': [],
  'steps-back/honest': [],
  'steps-back/hostile': [5],
  'steps-forward/honest': [],
  'steps-forward/hostile': [],
  'steps/honest': [],
  'steps/hostile': [5],
}
