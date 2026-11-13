import { watch } from 'vue'
import type { Message } from '../../types/domain'
import { EndSchedule } from '../../shared/lib/lifetime'
import { serverOffset } from '../../shared/lib/clock'
import type { Cx } from './state'
import { handleWSEvent } from './events'

// Messages with a lifetime leave the lists at their end on this client's own
// clock (corrected to the server's), as if the server's message_delete had
// come: the sweep may be a moment late, and a client that missed the event
// must not keep showing the message (doc/timed-messages.md). The server's own
// event, when it comes, finds nothing left to remove.

interface Ending { id: string; channel_id: string; parent_id: string | null }

// Browsers fire a timer of more than about 24.8 days at once: wait in steps.
const MAX_TIMER_MS = 2 ** 31 - 1

/** Watches the loaded messages of cx and removes each at its end. Returns a stop function. */
export function installExpiry(cx: Cx): () => void {
  const { cs } = cx
  const schedule = new EndSchedule<Ending>()
  let timer: ReturnType<typeof setTimeout> | null = null
  const serverNow = () => Date.now() + serverOffset.value

  function fire() {
    timer = null
    for (const m of schedule.due(serverNow())) {
      handleWSEvent(cx, { type: 'message_delete', payload: { id: m.id, channel_id: m.channel_id, parent_id: m.parent_id } })
    }
    arm()
  }

  function arm() {
    if (timer) clearTimeout(timer)
    timer = null
    const next = schedule.next()
    if (next === null) return
    timer = setTimeout(fire, Math.min(Math.max(0, next - serverNow()), MAX_TIMER_MS))
  }

  function track(list: readonly (Message | null | undefined)[]) {
    let changed = false
    for (const m of list) {
      if (!m?.expires_at) continue
      schedule.set(m.id, Date.parse(m.expires_at), { id: m.id, channel_id: m.channel_id, parent_id: m.parent_id ?? null })
      changed = true
    }
    if (changed) arm()
  }

  const stop = watch(
    [() => cs.messages.value, () => cs.threadReplies.value, () => cs.threadReplies.value.length, () => cs.activeThread.value],
    ([messages, replies, , root]) => track([...messages, ...replies, root as Message | null]),
    { immediate: true, deep: false }
  )
  return () => {
    stop()
    if (timer) clearTimeout(timer)
    schedule.clear()
  }
}
