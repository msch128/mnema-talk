// "Invite people" in a channel's menus: who is offered it, and the one dialog it opens.
import { shallowRef } from 'vue'
import { UserPlus } from '@lucide/vue'
import type { ContextMenuItem } from '../../shared/components/menuTypes'
import { useServerFeaturesStore } from '../../stores/serverFeatures'
import { usePermissionsStore } from '../../stores/permissions'
import { t } from '../../i18n'

/** The channel the invite dialog is open for; one host in App.vue shows it. */
export const channelInviteFor = shallowRef<string | null>(null)

export function openChannelInvite(channelId: string) {
  channelInviteFor.value = channelId
}

export function closeChannelInvite() {
  channelInviteFor.value = null
}

/** The server offers member invites and the member holds Create invites in the channel (the server decides again on every request). */
export function canInviteInto(channelId: string): boolean {
  return useServerFeaturesStore().memberInvites && usePermissionsStore().canIn(channelId, 'create_member_invites')
}

/** The "Invite people" entry of a channel's menu; none for a member who may not. */
export function inviteMenuItems(channelId: string): ContextMenuItem[] {
  if (!canInviteInto(channelId)) return []
  return [{ id: 'invite-people', label: t('memberInvites.menu'), icon: UserPlus, action: () => openChannelInvite(channelId) }]
}
