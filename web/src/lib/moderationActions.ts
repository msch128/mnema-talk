// What the signed-in member may do to another member, and doing it. Shared by
// the member context menu and the profile so both offer the same things. The
// server decides again on every request; this only decides what to offer.
import { api, caughtErrorMessage } from './api'
import { moderate } from './moderation'
import { useChatStore } from '../stores/chat'
import { useAuthStore } from '../stores/auth'
import { usePermissionsStore } from '../stores/permissions'
import { useToastStore } from '../stores/toast'
import { t } from '../i18n'

export interface ModerationSubject { id: string; role?: string | undefined; display_name?: string | undefined; username?: string | undefined }

export interface ModerationOffer {
  /** Time the member out (or change a running timeout). */
  timeout: boolean
  /** The member is timed out and the caller may lift it. */
  removeTimeout: boolean
  ban: boolean
}

export function moderationName(m: ModerationSubject): string {
  return m.display_name || m.username || m.id
}

/** Moderation needs the permission and a higher rank; never oneself, never the Owner. */
export function moderationOffer(member: ModerationSubject): ModerationOffer {
  const permissions = usePermissionsStore()
  const self = useAuthStore().user?.id === member.id
  const reachable = !self && member.role !== 'admin' && permissions.outranksMember(member.id)
  const until = useChatStore().members.find(m => m.id === member.id)?.timeout_until
  const timedOut = !!until && Date.parse(until) > Date.now()
  const canTimeout = reachable && permissions.can('timeout_members')
  return { timeout: canTimeout, removeTimeout: canTimeout && timedOut, ban: reachable && permissions.can('ban_members') }
}

export function openTimeout(member: ModerationSubject) {
  return moderate('timeout', { id: member.id, name: moderationName(member) })
}

export function openBan(member: ModerationSubject) {
  return moderate('ban', { id: member.id, name: moderationName(member) })
}

export async function liftTimeout(member: ModerationSubject): Promise<boolean> {
  const toasts = useToastStore()
  try {
    await api(`/api/admin/users/${member.id}/timeout`, { method: 'DELETE' })
    toasts.success(t('moderation.timeoutRemovedToast', { name: moderationName(member) }))
    return true
  } catch (e) {
    toasts.error(caughtErrorMessage(e, t('moderation.failed')))
    return false
  }
}
