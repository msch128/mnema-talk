// Using a component of a bot message or note. One use at a time per message:
// while it is on its way the used component is busy and the others are off.
// The server answers 202 with the interaction's id and hands the click to the bot.
// From then on a quiet line says what is going on:
//   - "<bot> is working…" until the bot's answer shows up (stores/chat/botAnswers.ts
//     says what counts as one), at most ANSWER_WAIT_MS;
//   - "<bot> is taking longer…" once the bot deferred this very interaction
//     (interaction_deferred, matched by its id): it promised an answer, so the
//     wait goes on, up to the interaction's lifetime (DEFERRED_WAIT_MS);
//   - "<bot> did not answer" when either wait runs out; the components can be used again.
// An answer that comes after that still clears the line. A refusal (404, 409, 429) is said at once.
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { api } from '../../shared/lib/api'
import { isRecord } from '../../types/validation'
import { useChatStore } from '../../stores/chat'
import { t } from '../../i18n'
import { ANSWER_WAIT_MS, DEFERRED_WAIT_MS, clickError } from '../lib/components'

/** Where the component is: its route, its channel, the message or note it is on, and its bot. */
export interface ComponentTarget { endpoint: string; channelId: string; messageId: string; botName: string }

type Phase = 'idle' | 'sending' | 'waiting' | 'deferred' | 'late'

/** The id of the interaction a 202 names, or '' for an older server that names none. */
function interactionIdOf(reply: unknown): string {
  return isRecord(reply) && typeof reply['interaction_id'] === 'string' ? reply['interaction_id'] : ''
}

export function useComponentUse(target: () => ComponentTarget) {
  const chat = useChatStore()
  const pending = ref<string | null>(null)
  const phase = ref<Phase>('idle')
  const error = ref('')
  let timer: ReturnType<typeof setTimeout> | null = null
  // The answer counter when the click went out: only a later answer counts.
  let clickedAt = 0
  let interactionId = ''

  function stopTimer() {
    if (timer) clearTimeout(timer)
    timer = null
  }

  function settle(message = '') {
    stopTimer()
    pending.value = null
    phase.value = 'idle'
    error.value = message
  }

  function wait(next: 'waiting' | 'deferred', ms: number) {
    stopTimer()
    phase.value = next
    // The components are free again; a late answer still clears the line (phase 'late').
    timer = setTimeout(() => {
      settle(t('components.noAnswer', { bot: target().botName }))
      phase.value = 'late'
    }, ms)
  }

  // An answer of this bot in this channel, newer than the click (botAnswers.ts says why not more exact).
  function answered(): boolean {
    const answer = chat.botAnswer
    if (!answer || answer.seq <= clickedAt) return false
    const { channelId, messageId, botName } = target()
    return answer.channelId === channelId && (answer.messageId === messageId || (botName !== '' && answer.botName === botName))
  }
  watch(() => chat.botAnswer, () => { if (phase.value !== 'idle' && phase.value !== 'sending' && answered()) settle() })
  watch(() => chat.botDeferral, deferral => {
    if (phase.value === 'waiting' && interactionId !== '' && deferral?.interactionId === interactionId) wait('deferred', DEFERRED_WAIT_MS)
  })

  /** Sends one use; false when it was refused or another one is still on its way. */
  async function use(customId: string, values?: string[]): Promise<boolean> {
    if (pending.value !== null) return false
    pending.value = customId
    phase.value = 'sending'
    error.value = ''
    clickedAt = chat.botAnswer?.seq ?? 0
    const deferredAt = chat.botDeferral?.seq ?? 0
    let reply: unknown
    try {
      reply = await api(target().endpoint, { method: 'POST', json: values ? { custom_id: customId, values } : { custom_id: customId } })
    } catch (e) {
      settle(clickError(e, target().botName))
      return false
    }
    interactionId = interactionIdOf(reply)
    // A fast bot may have answered (or deferred) before the 202 arrived.
    if (answered()) {
      settle()
      return true
    }
    const early = chat.botDeferral
    const deferred = !!early && early.seq > deferredAt && interactionId !== '' && early.interactionId === interactionId
    wait(deferred ? 'deferred' : 'waiting', deferred ? DEFERRED_WAIT_MS : ANSWER_WAIT_MS)
    return true
  }

  const STATUS: Partial<Record<Phase, string>> = { waiting: 'components.working', deferred: 'components.takingLonger' }
  const status = computed(() => {
    const key = STATUS[phase.value]
    return key ? t(key, { bot: target().botName }) : error.value
  })
  onBeforeUnmount(stopTimer)
  return { pending, status, use }
}
