// The failure budget of the deep sweeps of the server powers (seeds 1..200) and of the clocks per member (seeds
// 1..120): the seeds that break an invariant under each power or clock plan, per relay, measured on a local merge of followup-churn-2 (origin/main 3.0.9 differs from it by 43 lines of protocol code; to be measured again on main).
// Like groupSim.known.fixture.ts the lists may only shrink: a listed seed that passes turns the sweep red until it
// leaves the list, a new failing seed turns it red until it is explained. What is left: slow recovery after roster
// lies (seed 160 agrees after 262 s, not within 240), and every plan with wall clock steps BACK, which loses members
// through the pacer (pacer.step.test.ts, finding H-L2-05); a step forward costs nothing.
export const KNOWN_POWERS: Readonly<Record<string, readonly number[]>> = {
  'liar/honest': [],
  'liar/hostile': [160],
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
  'all/honest': [1, 5, 9, 10, 11, 14, 22, 25, 30, 33, 36, 38, 39, 41, 47, 52, 53, 57, 58, 59, 74, 80, 85, 90, 94, 102, 104, 113, 117, 120],
  'all/hostile': [1, 4, 5, 10, 13, 14, 25, 28, 30, 31, 33, 37, 41, 43, 47, 49, 57, 58, 60, 62, 71, 74, 75, 82, 97, 98, 105, 107, 113, 116, 119],
  'drift/honest': [],
  'drift/hostile': [],
  'extreme/honest': [],
  'extreme/hostile': [6, 120],
  'none/honest': [],
  'none/hostile': [],
  'offset/honest': [],
  'offset/hostile': [],
  'steps-back/honest': [1, 2, 5, 10, 11, 13, 14, 21, 22, 29, 30, 36, 38, 39, 41, 43, 51, 53, 57, 58, 59, 61, 73, 74, 76, 77, 80, 81, 85, 90, 91, 92, 95, 100, 102, 109, 112, 113, 117, 119, 120],
  'steps-back/hostile': [1, 3, 4, 5, 9, 12, 13, 14, 17, 18, 21, 22, 25, 26, 30, 31, 33, 36, 37, 41, 58, 60, 61, 62, 66, 75, 77, 84, 85, 89, 91, 100, 101, 105, 107, 108, 109, 111, 113, 114, 117, 120],
  'steps-forward/honest': [],
  'steps-forward/hostile': [],
  'steps/honest': [1, 5, 9, 10, 11, 14, 22, 25, 28, 30, 33, 36, 38, 39, 41, 47, 52, 53, 57, 58, 59, 74, 80, 85, 90, 94, 95, 102, 107, 111, 113, 117],
  'steps/hostile': [1, 4, 5, 12, 13, 14, 22, 25, 30, 31, 36, 37, 41, 47, 49, 57, 58, 59, 60, 61, 74, 75, 82, 105, 107, 108, 113, 116, 119, 120],
}
