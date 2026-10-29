import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { noticeReminderDue, noticeScheduledFailed } from './laterNotices'
import { useChatStore } from '../stores/chat'
import { useToastStore } from '../stores/toast'
import { setLocale, t } from '../i18n'
import { fixtureId, FIXTURE_TIMESTAMP } from '../test-fixtures.fixture'

beforeEach(() => {
  setLocale('en')
  setActivePinia(createPinia())
})

const entry = (snippet: string | null) => ({
  id: fixtureId(1), remind_at: FIXTURE_TIMESTAMP, created_at: FIXTURE_TIMESTAMP, delivered_at: null, available: snippet !== null,
  message: snippet === null ? null : { id: fixtureId(40), channel_id: fixtureId(2), parent_id: null, channel_name: 'general', dm: false, author: 'Alice', snippet }
}) as never

describe('reminder and scheduled message notices', () => {
  it('names the message a reminder is about when it is still visible', () => {
    noticeReminderDue(entry('call mum'))
    expect(useToastStore().toasts.at(-1)?.text).toBe(t('later.reminderDueWith', { text: 'call mum' }))
  })

  it('offers to open the message of a reminder and lands on it', async () => {
    const go = vi.spyOn(useChatStore(), 'goToMessage').mockImplementation(() => undefined)
    noticeReminderDue(entry('call mum'))
    const action = useToastStore().toasts.at(-1)?.action
    expect(action?.label).toBe(t('later.openMessage'))
    await action?.onClick()
    expect(go).toHaveBeenCalledWith({ id: fixtureId(40), channel_id: fixtureId(2) })
  })

  it('has nothing to open when the message is gone', () => {
    noticeReminderDue(entry(null))
    expect(useToastStore().toasts.at(-1)?.action).toBeNull()
  })

  it('falls back to a plain notice when the message is gone or the reminder unknown', () => {
    noticeReminderDue(entry(null))
    expect(useToastStore().toasts.at(-1)?.text).toBe(t('later.reminderDue'))
    noticeReminderDue(undefined)
    expect(useToastStore().toasts.at(-1)?.text).toBe(t('later.reminderDue'))
  })

  it('explains known refusals and shows anything else as a plain refusal', () => {
    for (const reason of ['slow_mode', 'not_allowed', 'unavailable']) {
      noticeScheduledFailed(reason)
      expect(useToastStore().toasts.at(-1)?.detail).toBe(t(`later.reason.${reason}`))
    }
    noticeScheduledFailed(undefined)
    expect(useToastStore().toasts.at(-1)?.detail).toBe(t('later.reason.refused'))
    noticeScheduledFailed('')
    expect(useToastStore().toasts.at(-1)?.detail).toBe(t('later.reason.refused'))
  })
})
