// Who a call is with, for the cards that show it: the caller, and what the
// conversation is called. The members come from the conversation; a caller this
// client does not know yet shows as an unnamed member until it does.
import { computed, type Ref } from 'vue'
import { useAuthStore } from '../stores/auth'
import { useChatStore } from '../stores/chat'
import { dmPeers, dmTitle, nameOf } from '../lib/dm'
import type { CallInfo } from '../stores/dmCalls'

export function useCallParty(call: Ref<CallInfo>) {
  const chat = useChatStore()
  const auth = useAuthStore()
  const me = computed(() => auth.user?.id)
  const dm = computed(() => chat.dms.find(d => d.id === call.value.conversationId) ?? null)
  const caller = computed(() => dm.value?.members.find(m => m.id === call.value.callerId) ?? null)
  const callerName = computed(() => (caller.value ? nameOf(caller.value) : ''))
  /** The others in the conversation: the person called, or the group. */
  const peers = computed(() => (dm.value ? dmPeers(dm.value, me.value) : []))
  const title = computed(() => (dm.value ? dmTitle(dm.value, me.value) : ''))
  const group = computed(() => dm.value?.group === true)
  return { dm, caller, callerName, peers, title, group }
}
