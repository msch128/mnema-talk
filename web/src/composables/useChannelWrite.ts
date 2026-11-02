import { computed } from 'vue'
import type { Ref } from 'vue'
import { useChatStore } from '../stores/chat'
import { usePermissionsStore } from '../stores/permissions'
import { waitSentence } from '../lib/newMember'
import { t } from '../i18n'

/**
 * What the member may do in the open channel and what its composer says about
 * it; the server decides, this only hides what would fail. A conversation
 * (DM) answers for itself; a running timeout is named instead of a plain
 * "you can't write here".
 */
export function useChannelWrite(dmChat: {
  isDm: Ref<boolean>
  canSend: Ref<boolean>
  placeholder: Ref<string>
}, panel: () => boolean) {
  const chat = useChatStore()
  const permissions = usePermissionsStore()
  const canIn = (key: 'send_messages' | 'attach_files' | 'add_reactions') => permissions.canIn(chat.activeChannel?.id ?? '', key)
  const canSend = computed(() => (dmChat.isDm.value ? dmChat.canSend.value : canIn('send_messages')))
  const canAttach = computed(() => canSend.value && (dmChat.isDm.value || canIn('attach_files')))
  const canReact = computed(() => (dmChat.isDm.value ? dmChat.canSend.value : canIn('add_reactions')))
  const timedOut = computed(() => permissions.timeoutUntil !== null && permissions.timeoutUntil > Date.now())
  const placeholder = computed(() => {
    if (dmChat.isDm.value) return dmChat.placeholder.value
    if (canSend.value) return t(panel() ? 'talk.chatPlaceholder' : 'chat.placeholder', { channel: chat.activeChannel?.name ?? '' })
    if (timedOut.value) return t('chat.timedOut')
    const wait = permissions.newMemberWait('write')
    return wait > 0 ? waitSentence('write', wait) : t('chat.readOnly')
  })
  return { canSend, canAttach, canReact, placeholder }
}
