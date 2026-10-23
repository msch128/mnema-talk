// What the chat area does differently when the open channel slot holds a
// direct message conversation: who may write there, what the empty state and
// the composer say, and what a refused send reads like.
import { computed } from 'vue'
import { useChatStore } from '../stores/chat'
import { caughtErrorMessage, isApiError } from '../lib/api'
import { dmErrorText } from '../lib/dm'
import { t } from '../i18n'

export function useDmChat() {
  const chatStore = useChatStore()
  const dm = computed(() => chatStore.dmById(chatStore.activeChannel?.id))
  const isDm = computed(() => dm.value !== null)
  const name = computed(() => chatStore.activeChannel?.name ?? '')

  /** The server's own answer: the permission, the community switch and any timeout in one flag. */
  const canSend = computed(() => dm.value?.can_send === true)
  const welcome = computed(() => t(dm.value?.group ? 'dm.groupWelcome' : 'dm.welcome', { name: name.value }))
  const beginning = computed(() => t(dm.value?.group ? 'dm.groupBeginning' : 'dm.beginning', { name: name.value }))
  const placeholder = computed(() => t(canSend.value ? 'dm.placeholder' : 'dm.readOnly', { name: name.value }))

  /** The words for a failed send: the calm refusal in a conversation, the usual text elsewhere. */
  function sendError(error: unknown, fallback: string): string {
    const refused = isApiError(error) && (error.status === 403 || error.status === 429)
    return isDm.value && refused ? dmErrorText(error, 'send') : caughtErrorMessage(error, fallback)
  }

  return { dm, isDm, canSend, welcome, beginning, placeholder, sendError }
}
