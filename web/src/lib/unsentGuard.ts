import { confirm } from '../shared/lib/confirm'
import { t } from '../i18n'
import { useAuthStore } from '../stores/auth'
import { useChatStore } from '../stores/chat'

/** How many of the signed-in account's messages are still waiting in the outbox (sent or refused ones are gone from it already). */
export function unsentCount(): number {
  const uid = useAuthStore().user?.id
  return useChatStore().outbox.filter(p => p.userId === uid).length
}

/**
 * Signing out or deleting the account throws the outbox away. With messages in it, ask first;
 * without, answer yes at once so a plain sign-out stays one click.
 */
export async function confirmDiscardUnsent(confirmLabel: string): Promise<boolean> {
  const count = unsentCount()
  if (!count) return true
  return confirm({
    title: t('outbox.discardOnLeaveTitle', { count }),
    body: t('outbox.discardOnLeaveBody'),
    confirmLabel, danger: true
  })
}
