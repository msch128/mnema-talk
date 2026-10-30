// The retention periods a channel's auto-delete can be set to (internal/chat/auto_delete.go).
import { confirm } from './confirm'
import { t } from '../i18n'

export const AUTO_DELETE_DAYS = [1, 7, 30, 90] as const

/** Turning auto-delete on destroys messages, so it asks first; turning it off does not. */
export function confirmAutoDelete(channelName: string, days: number): Promise<boolean> {
  if (days <= 0) return Promise.resolve(true)
  return confirm({
    title: t('autoDelete.confirmTitle'),
    body: t('autoDelete.confirmBody', { name: channelName, period: t(`autoDelete.periods.${days}`) }),
    confirmLabel: t('autoDelete.confirmAction'),
    danger: true
  })
}
