import { ref, watch } from 'vue'
import type { Ref } from 'vue'
import { nextOpening, waitLeft } from '../lib/newMember'
import type { NewMemberAction, NewMemberTimes } from '../lib/newMember'
import { visibleInterval } from '../lib/pageVisibility'

const MAX_DELAY_MS = 2 ** 31 - 1

/**
 * A new member's waits end by themselves (raid protection): while one is
 * pending, `clock` runs so the hints count down, and `reload` is called at the
 * moment the first wait ends, which gives the member the right back.
 */
export function useNewMemberClock(times: Readonly<Ref<NewMemberTimes>>, reload: () => Promise<void>) {
  const clock = ref(Date.now())
  let timer: ReturnType<typeof setTimeout> | undefined
  let stopTick: (() => void) | undefined

  function stop() {
    clearTimeout(timer)
    stopTick?.()
    stopTick = undefined
  }

  watch(times, next => {
    stop()
    clock.value = Date.now()
    const opening = nextOpening(next, clock.value)
    if (opening === null) return
    stopTick = visibleInterval(() => { clock.value = Date.now() }, 20_000)
    timer = setTimeout(() => { void reload() }, Math.min(opening - clock.value + 500, MAX_DELAY_MS))
  }, { immediate: true })

  /** Milliseconds still to wait for an action; 0 when it is open. */
  const wait = (action: NewMemberAction) => waitLeft(times.value, action, clock.value)

  return { wait, stop }
}
