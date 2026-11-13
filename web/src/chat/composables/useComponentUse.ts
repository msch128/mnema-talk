// Using a component of a bot message or note. One use at a time per message:
// while it is on its way the used component is busy and the others are off.
// The server answers 202 at once and hands the click to the bot; from then on a
// quiet "<bot> is working…" line stays until the bot's answer shows up
// (stores/chat/botAnswers.ts says what counts as one), or, after ANSWER_WAIT_MS
// without one, "<bot> did not answer" and the components can be used again.
// A refusal (404, 409, 429) is said at once.
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { api } from '../../shared/lib/api'
import { useChatStore } from '../../stores/chat'
import { t } from '../../i18n'
import { ANSWER_WAIT_MS, clickError } from '../lib/components'

/** Where the component is: its route, its channel, the message or note it is on, and its bot. */
export interface ComponentTarget { endpoint: string; channelId: string; messageId: string; botName: string }

export function useComponentUse(target: () => ComponentTarget) {
  const chat = useChatStore()
  const pending = ref<string | null>(null)
  const waiting = ref(false)
  const error = ref('')
  let timer: ReturnType<typeof setTimeout> | null = null
  // The answer counter when the click went out: only a later answer ends the wait.
  let clickedAt = 0

  function settle(message = '') {
    if (timer) clearTimeout(timer)
    timer = null
    pending.value = null
    waiting.value = false
    error.value = message
  }

  // An answer of this bot in this channel, newer than the click (botAnswers.ts says why not more exact).
  function answered(): boolean {
    const answer = chat.botAnswer
    if (!answer || answer.seq <= clickedAt) return false
    const { channelId, messageId, botName } = target()
    return answer.channelId === channelId && (answer.messageId === messageId || (botName !== '' && answer.botName === botName))
  }
  watch(() => chat.botAnswer, () => { if (waiting.value && answered()) settle() })

  /** Sends one use; false when it was refused or another one is still on its way. */
  async function use(customId: string, values?: string[]): Promise<boolean> {
    if (pending.value !== null) return false
    pending.value = customId
    error.value = ''
    clickedAt = chat.botAnswer?.seq ?? 0
    try {
      await api(target().endpoint, { method: 'POST', json: values ? { custom_id: customId, values } : { custom_id: customId } })
    } catch (e) {
      settle(clickError(e, target().botName))
      return false
    }
    // A fast bot may have answered before the 202 arrived.
    if (answered()) {
      settle()
      return true
    }
    waiting.value = true
    timer = setTimeout(() => settle(t('components.noAnswer', { bot: target().botName })), ANSWER_WAIT_MS)
    return true
  }

  const status = computed(() => (waiting.value ? t('components.working', { bot: target().botName }) : error.value))
  onBeforeUnmount(() => { if (timer) clearTimeout(timer) })
  return { pending, status, use }
}
