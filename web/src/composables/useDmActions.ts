// "Message" and "Block" for a member, shared by the profile card and the
// member menus. Everything is hidden on a server without direct messages or
// blocking (GET /api/health features).
import { computed } from 'vue'
import { useAuthStore } from '../stores/auth'
import { useBlocksStore } from '../stores/blocks'
import { useChatStore } from '../stores/chat'
import { useServerFeaturesStore } from '../stores/serverFeatures'
import { usePermissionsStore } from '../stores/permissions'
import { useToastStore } from '../stores/toast'
import { dmErrorText } from '../lib/dm'
import { t } from '../i18n'

export function useDmActions() {
  const auth = useAuthStore()
  const blocks = useBlocksStore()
  const chat = useChatStore()
  const features = useServerFeaturesStore()
  const permissions = usePermissionsStore()
  const toasts = useToastStore()

  /** Whether this member may start conversations now (the community switch, the permission, no timeout). */
  const canStart = computed(() => features.dm && permissions.info?.can_start_direct_messages === true)

  /** Offer "Message" for this member: not oneself, not a bot, not somebody the caller blocked. */
  function canMessage(userId: string | null | undefined): boolean {
    if (!canStart.value || !userId || userId === auth.user?.id) return false
    if (chat.members.find(m => m.id === userId)?.is_bot) return false
    return !blocks.isBlocked(userId)
  }

  function canBlock(userId: string | null | undefined): boolean {
    return features.blocks && !!userId && userId !== auth.user?.id
  }

  /** Opens the conversation with this member, starting it when there is none yet. */
  async function messageMember(userId: string): Promise<boolean> {
    try {
      const dm = await chat.startDm([userId])
      await chat.openDm(dm.id)
      return true
    } catch (error) {
      toasts.error(dmErrorText(error, 'start'))
      return false
    }
  }

  /** Blocks the member, or lifts the block; says so in a toast. */
  async function toggleBlock(user: { id: string; display_name?: string | undefined; username?: string | undefined }) {
    const name = user.display_name || user.username || ''
    const blocked = blocks.isBlocked(user.id)
    if (!(await (blocked ? blocks.unblock(user.id) : blocks.block(user.id)))) return
    toasts.success(t(blocked ? 'blocks.unblocked' : 'blocks.blocked', { name }))
  }

  return { canStart, canMessage, canBlock, messageMember, toggleBlock }
}
