/**
 * Runs `task` now and, while it reports failure, once more after each wait in
 * `waits` (so the list bounds the retries). Stops early when `keepGoing` says
 * the result is no longer wanted. True when the task succeeded.
 */
export async function retryWithBackoff(
  task: () => Promise<boolean>,
  waits: readonly number[],
  keepGoing: () => boolean = () => true
): Promise<boolean> {
  for (const wait of [0, ...waits]) {
    if (wait) await new Promise(resolve => setTimeout(resolve, wait))
    if (!keepGoing()) return false
    if (await task()) return true
  }
  return false
}
