import { defineStore } from 'pinia'
import { computed, reactive, ref } from 'vue'
import { api, isApiError } from '../lib/api'
import { isChatBlockedMember, type ChatBlockedMember } from '../types/rest'
import { arrayDecoder } from '../types/validation'
import { t } from '../i18n'
import { useServerFeaturesStore } from './serverFeatures'
import { useToastStore } from './toast'

const decodeBlocked = arrayDecoder('blocked members', isChatBlockedMember)

// The members this member blocked (GET /api/me/blocks). Blocking is private:
// the list belongs to this member's sessions only and nothing about it is
// told to the blocked member. Their messages are collapsed in every list.
export const useBlocksStore = defineStore('blocks', () => {
  const features = useServerFeaturesStore()
  const blocked = ref<ChatBlockedMember[]>([])
  const ids = computed(() => new Set(blocked.value.map(b => b.user_id)))
  // Blocked members' messages the reader chose to show (kept until the page closes).
  const revealed = reactive(new Set<string>())
  let loadSeq = 0

  const enabled = computed(() => features.blocks)
  const isBlocked = (userId: string | null | undefined) => enabled.value && !!userId && ids.value.has(userId)
  /** Whether a message is shown as a collapsed line instead of its text. */
  const hides = (msg: { id: string; user_id?: string | undefined }) => isBlocked(msg.user_id) && !revealed.has(msg.id)

  async function load() {
    if (!enabled.value) return
    const seq = ++loadSeq
    try {
      const list = await api('/api/me/blocks', { decode: decodeBlocked })
      if (seq === loadSeq) blocked.value = list
    } catch (error) {
      console.warn('Failed to read the block list:', error)
    }
  }

  async function change(userId: string, method: 'PUT' | 'DELETE', failed: string): Promise<boolean> {
    try {
      await api(`/api/me/blocks/${userId}`, { method })
      if (method === 'DELETE') revealed.clear()
      await load()
      return true
    } catch (error) {
      useToastStore().error(isApiError(error) && error.status === 409 ? t('blocks.full') : t(failed))
      return false
    }
  }

  const block = (userId: string) => change(userId, 'PUT', 'blocks.blockFailed')
  const unblock = (userId: string) => change(userId, 'DELETE', 'blocks.unblockFailed')
  const reveal = (messageId: string) => { revealed.add(messageId) }

  function reset() {
    ++loadSeq
    blocked.value = []
    revealed.clear()
  }

  return { blocked, enabled, isBlocked, hides, load, block, unblock, reveal, reset }
})
