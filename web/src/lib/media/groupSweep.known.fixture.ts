// The failure budget of the deep sweeps of the server powers (seeds 1..200) and of the clocks per member (seeds
// 1..120): the seeds that break an invariant under each power or clock plan, per relay, as measured at origin/main
// (56f6a4101 plus this harness). Like groupSim.known.fixture.ts the lists may only shrink: a listed seed that passes
// turns the sweep red until it leaves the list, a new failing seed turns it red until it is explained. Most of
// them are the removal defect (the same seeds pass with followup-churn-2); the clock plans with steps add the
// wall-clock pacer (pacer.step.test.ts).
export const KNOWN_POWERS: Readonly<Record<string, readonly number[]>> = {
  'liar/honest': [25, 36, 88, 108, 167],
  'liar/hostile': [20, 33, 67, 80, 91, 102, 104, 107, 109, 122, 126, 160, 178, 185, 196],
  'replay-commit/honest': [30, 64, 109, 116, 119, 125, 148, 162, 167],
  'replay-commit/hostile': [30, 43, 59, 73, 89, 97, 104, 107, 133, 148, 150, 194, 199],
  'replay-harmless/honest': [30, 64, 109, 116, 119, 125, 148, 162, 167],
  'replay-harmless/hostile': [30, 43, 59, 73, 89, 97, 104, 107, 133, 148, 150, 194, 199],
  'replay-key-package/honest': [30, 64, 82, 107, 109, 116, 119, 125, 148, 162, 167, 174, 179, 180],
  'replay-key-package/hostile': [25, 30, 33, 43, 58, 59, 64, 69, 73, 84, 89, 97, 104, 107, 133, 142, 145, 148, 150, 166, 180, 182, 193, 194, 199],
  'replay/honest': [30, 64, 109, 116, 119, 125, 148, 162, 167, 180, 186],
  'replay/hostile': [30, 33, 43, 49, 59, 67, 73, 89, 97, 104, 107, 133, 145, 148, 150, 165, 187, 194, 199],
}

export const KNOWN_CLOCKS: Readonly<Record<string, readonly number[]>> = {
  'all/honest': [1, 5, 9, 10, 11, 14, 22, 25, 26, 30, 36, 39, 43, 47, 52, 53, 57, 58, 70, 74, 80, 82, 90, 94, 98, 102, 104, 116, 117, 119, 120],
  'all/hostile': [1, 4, 5, 10, 13, 14, 16, 20, 25, 30, 31, 33, 37, 41, 43, 47, 49, 57, 58, 60, 63, 67, 71, 73, 74, 80, 82, 97, 98, 104, 105, 107, 109, 113, 116, 119, 120],
  'drift/honest': [30, 81, 111, 116, 119],
  'drift/hostile': [30, 64, 67, 80, 97, 102, 107, 112, 114, 120],
  'extreme/honest': [30, 80],
  'extreme/hostile': [6, 25, 30, 41, 43, 67, 77, 80, 82, 89, 97, 102, 107, 114, 120],
  'none/honest': [30, 64, 109, 116, 119],
  'none/hostile': [30, 43, 59, 73, 89, 97, 104, 107],
  'offset/honest': [30, 64, 109, 116, 119],
  'offset/hostile': [30, 43, 59, 73, 89, 97, 104, 107],
  'steps-back/honest': [1, 2, 5, 10, 11, 13, 14, 21, 22, 25, 30, 36, 37, 39, 41, 43, 51, 53, 57, 58, 61, 64, 73, 74, 76, 77, 80, 81, 82, 90, 91, 92, 100, 102, 109, 112, 116, 117, 119],
  'steps-back/hostile': [1, 4, 5, 9, 10, 11, 12, 13, 14, 17, 20, 21, 22, 26, 27, 30, 31, 33, 36, 37, 41, 43, 58, 60, 61, 62, 66, 67, 71, 73, 77, 81, 82, 85, 89, 91, 98, 100, 101, 105, 107, 108, 109, 111, 112, 113, 114, 116, 117, 120],
  'steps-forward/honest': [64, 119],
  'steps-forward/hostile': [20, 33, 43, 58, 67, 71, 73, 89, 102, 104, 107, 109],
  'steps/honest': [1, 5, 9, 10, 11, 14, 22, 25, 28, 30, 36, 39, 43, 47, 52, 53, 57, 58, 73, 74, 80, 90, 94, 102, 107, 109, 116, 117, 119],
  'steps/hostile': [1, 4, 5, 10, 11, 12, 13, 14, 22, 30, 31, 33, 36, 37, 41, 47, 49, 57, 58, 60, 61, 62, 64, 67, 71, 73, 74, 80, 82, 98, 104, 105, 107, 108, 109, 113, 116, 119, 120],
}
