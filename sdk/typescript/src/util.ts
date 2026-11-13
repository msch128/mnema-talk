/** Drops undefined fields so the JSON carries only what was set. */
export function compact<T extends object>(o: T): T {
  return Object.fromEntries(Object.entries(o).filter(([, v]) => v !== undefined)) as T
}

/**
 * The URL without trailing slashes. A loop, not a regular expression: a
 * pattern anchored at the end with a repeated slash backtracks over every run
 * of slashes that is not at the end, quadratic on input such as many slashes
 * followed by another character.
 */
export function trimSlash(url: string): string {
  let end = url.length
  while (end > 0 && url.charCodeAt(end - 1) === 0x2f) end--
  return url.slice(0, end)
}

// setTimeout fires at once for anything above 2^31-1 ms.
const MAX_TIMER_MS = 2 ** 31 - 1

/** A promise that resolves after ms (at most 2^31-1); the timer is cleared when signal aborts. */
export function sleep(ms: number, signal?: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    if (signal?.aborted) return reject(signal.reason)
    const onAbort = () => {
      clearTimeout(timer)
      reject(signal?.reason)
    }
    const timer = setTimeout(() => {
      signal?.removeEventListener('abort', onAbort)
      resolve()
    }, Math.min(Math.max(0, ms), MAX_TIMER_MS))
    signal?.addEventListener('abort', onAbort, { once: true })
  })
}

export interface Logger {
  debug?(message: string, ...args: unknown[]): void
  info?(message: string, ...args: unknown[]): void
  warn?(message: string, ...args: unknown[]): void
  error?(message: string, ...args: unknown[]): void
}
