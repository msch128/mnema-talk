// The session check at start-up. The check answers null when the server can't
// tell (rate limited, down, no network): that is not "signed out", so it keeps
// trying with a growing pause, and "Try again" cuts the pause short.
import { ref } from 'vue'

const MAX_PAUSE_MS = 15000

export function useSessionCheck<R>(check: () => Promise<R | null>, firstPause = 1000) {
  /** The server did not answer at least once: show the unreachable screen. */
  const retrying = ref(false)
  /** A check is running right now. */
  const trying = ref(false)
  /** When the last check finished without an answer. */
  const lastTriedAt = ref<number | null>(null)
  let alive = true
  let timer: ReturnType<typeof setTimeout> | undefined
  let wake: (() => void) | undefined

  async function attempt(): Promise<R | null> {
    trying.value = true
    try {
      const result = await check()
      if (result === null) lastTriedAt.value = Date.now()
      return result
    } finally {
      trying.value = false
    }
  }

  async function run(): Promise<R | null> {
    let pause = firstPause
    let result = await attempt()
    while (alive && result === null) {
      retrying.value = true
      await new Promise<void>(resolve => {
        wake = resolve
        timer = setTimeout(resolve, pause)
      })
      wake = undefined
      timer = undefined
      if (!alive) return null
      pause = Math.min(pause * 2, MAX_PAUSE_MS)
      result = await attempt()
    }
    if (!alive) return null
    retrying.value = false
    return result
  }

  /** Ends the pause now; a check that is already running is left to finish. */
  function retryNow() {
    wake?.()
  }

  function dispose() {
    alive = false
    clearTimeout(timer)
    wake?.()
  }

  return { retrying, trying, lastTriedAt, run, retryNow, dispose }
}
