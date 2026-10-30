import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { noticeReminderDue, noticeScheduledFailed } from './laterNotices'
import { useChatStore } from '../stores/chat'
import { useToastStore } from '../stores/toast'
import { setLocale, t } from '../i18n'
import { useAuthStore } from '../stores/auth'
import { platform } from './platform'
import { pushOnThisDevice } from './webPush'
import { userFixture } from '../test-fixtures.fixture'
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
    expect(useToastStore().toasts.at(-1)?.text).toBe(t('later.reminderDueChannel', { author: 'Alice', channel: 'general', text: 'call mum' }))
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

  it('says who wrote to you in a conversation, without a channel name', () => {
    const dm = { ...(entry('call mum') as { message: object }), message: { id: fixtureId(40), channel_id: fixtureId(2), parent_id: null, channel_name: '', dm: true, author: 'Alice', snippet: 'call mum' } }
    noticeReminderDue(dm as never)
    expect(useToastStore().toasts.at(-1)?.text).toBe(t('later.reminderDueDm', { author: 'Alice', text: 'call mum' }))
  })

  describe('when nobody is looking at the page', () => {
    const notify = vi.fn()

    beforeEach(() => {
      notify.mockReset()
      vi.spyOn(platform, 'notificationPermission').mockReturnValue('granted')
      vi.spyOn(platform, 'notify').mockImplementation(notify)
      vi.spyOn(document, 'hidden', 'get').mockReturnValue(true)
      pushOnThisDevice.value = false
      useAuthStore().user = userFixture({ presence: 'online' })
    })
    afterEach(() => vi.restoreAllMocks())

    it('raises an operating system notification that opens the message', () => {
      const goTo = vi.spyOn(useChatStore(), 'goToMessage').mockImplementation(() => {})
      noticeReminderDue(entry('call mum'))
      expect(notify).toHaveBeenCalledWith(expect.objectContaining({
        title: t('later.reminderTitle'), body: t('later.reminderDueChannel', { author: 'Alice', channel: 'general', text: 'call mum' }), channelId: fixtureId(2), messageId: fixtureId(40),
      }))
      notify.mock.calls[0]?.[0].onClick()
      expect(goTo).toHaveBeenCalled()
    })

    it('stays quiet with push on this device, in do-not-disturb, or without permission', () => {
      pushOnThisDevice.value = true
      noticeReminderDue(entry('a'))
      pushOnThisDevice.value = false
      useAuthStore().user = userFixture({ presence: 'dnd' })
      noticeReminderDue(entry('b'))
      useAuthStore().user = userFixture({ presence: 'online' })
      vi.spyOn(platform, 'notificationPermission').mockReturnValue('denied')
      noticeReminderDue(entry('c'))
      expect(notify).not.toHaveBeenCalled()
    })
  })
})
