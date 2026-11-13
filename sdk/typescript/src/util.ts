/** Drops undefined fields so the JSON carries only what was set. */
export function compact<T extends object>(o: T): T {
  return Object.fromEntries(Object.entries(o).filter(([, v]) => v !== undefined)) as T
}

/** The URL without trailing slashes. */
export function trimSlash(url: string): string {
  return url.replace(/\/+$/, '')
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
