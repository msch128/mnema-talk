// The failure budget of the deep sweeps of the server powers (seeds 1..200) and of the clocks per member (seeds
// 1..120): the seeds that break an invariant under each power or clock plan, per relay, measured on 2026-11-12 on the
// tree that ships: origin/main 3.0.9 with this harness, the fix of H-L2-04 and the fix of H-L2-05 and H-L9-02 (pacer
// and back-offs on the monotonic clock).
// Like groupSim.known.fixture.ts a listed seed that passes turns the sweep red until it leaves the list, and a new
// failing seed turns it red until it is explained. A list grows only with a written reason next to the seed; one has
// so far: liar/hostile 102, a slow tail observed on this tree.
export const KNOWN_POWERS: Readonly<Record<string, readonly number[]>> = {
  'liar/honest': [],
  // 102: agrees after 284 s against a limit of 240 s on the merged tree; one member holds an old epoch; cause not traced.
  'liar/hostile': [102],
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
  // 120: H-L2-06, equal-epoch fork.
  'all/honest': [120],
  // 5: H-L2-06, equal-epoch fork.
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
  // 5: H-L2-06, equal-epoch fork.
  'steps-back/hostile': [5],
  'steps-forward/honest': [],
  'steps-forward/hostile': [],
  'steps/honest': [],
  // 5: H-L2-06, equal-epoch fork.
  'steps/hostile': [5],
}
