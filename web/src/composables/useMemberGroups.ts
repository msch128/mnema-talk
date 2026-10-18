// How the member list is grouped: the Owner row, hoisted roles, online, offline.
import { computed } from 'vue'
import { t } from '../i18n'
import { useChatStore } from '../stores/chat'
import { usePermissionsStore } from '../stores/permissions'
import { useVoiceStore } from '../stores/voice'

export function useMemberGroups() {
  const chatStore = useChatStore()
  const voiceStore = useVoiceStore()
  const permissions = usePermissionsStore()

  const admins = computed(() => chatStore.members.filter(m => m.role === 'admin'))
  const onlineOthers = computed(() => chatStore.onlineMembers.filter(m => m.role !== 'admin'))

  // Online members whose highest hoisted role gets a group of its own, in the
  // rank order of the roles; everyone else online stays in "Online".
  const roleGroups = computed(() => {
    const hoistedOf = (id: string) => permissions.rolesOf(id).find(role => role.hoist)
    return permissions.roles
      .filter(role => role.hoist && !role.is_everyone)
      .map(role => ({ role, members: onlineOthers.value.filter(m => hoistedOf(m.id)?.id === role.id) }))
      .filter(group => group.members.length > 0)
  })

  const onlineNonAdmins = computed(() => {
    const grouped = new Set(roleGroups.value.flatMap(group => group.members.map(m => m.id)))
    return onlineOthers.value.filter(m => !grouped.has(m.id))
  })

  const offlineNonAdmins = computed(() => chatStore.offlineMembers.filter(m => m.role !== 'admin'))

  /** The name of the voice channel a member is in, or null when in none. */
  function voiceChannelName(userId: string) {
    const channelId = Object.keys(voiceStore.channelUsers).find(id => voiceStore.channelUsers[id]?.[userId])
    if (channelId === undefined) return null
    const channels = [...chatStore.categories.flatMap(cat => cat.channels ?? []), ...(chatStore.uncategorized ?? [])]
    return channels.find(c => c.id === channelId)?.name ?? t('voice.channelFallback')
  }

  return { admins, roleGroups, onlineNonAdmins, offlineNonAdmins, voiceChannelName }
}
