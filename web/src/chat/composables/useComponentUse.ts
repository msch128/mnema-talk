// Using a component of a bot message or note: one click at a time per message,
// a short pending state on the used component (the server answers 202 and the
// bot's answer follows as an event), and a quiet note when it was refused.
import { onBeforeUnmount, ref } from 'vue'
import { api } from '../../shared/lib/api'
import { clickError, PENDING_MS } from '../lib/components'

export function useComponentUse(endpoint: () => string, botName: () => string) {
  const pending = ref<string | null>(null)
  const error = ref('')
  let timer: ReturnType<typeof setTimeout> | null = null

  function clearTimer() {
    if (timer) clearTimeout(timer)
    timer = null
  }

  /** Sends one use; false when another one is still on its way. */
  async function use(customId: string, values?: string[]): Promise<boolean> {
    if (pending.value !== null) return false
    clearTimer()
    pending.value = customId
    error.value = ''
    try {
      await api(endpoint(), { method: 'POST', json: values ? { custom_id: customId, values } : { custom_id: customId } })
      timer = setTimeout(() => { pending.value = null }, PENDING_MS)
      return true
    } catch (e) {
      pending.value = null
      error.value = clickError(e, botName())
      return false
    }
  }

  onBeforeUnmount(clearTimer)
  return { pending, error, use }
}
