import type { ApiOptions } from '../lib/api'

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useLaterStore } from './later'
import { useToastStore } from './toast'
import { setLocale, t } from '../i18n'
import { fixtureId, FIXTURE_TIMESTAMP } from '../test-fixtures.fixture'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async importOriginal => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args)
}))

const ref = (snippet: string) => ({ id: fixtureId(40), channel_id: fixtureId(2), parent_id: null, channel_name: 'general', dm: false, author: 'Alice', snippet })
const reminder = (id: number, over: Record<string, unknown> = {}) => ({
  id: fixtureId(id), remind_at: FIXTURE_TIMESTAMP, created_at: FIXTURE_TIMESTAMP, delivered_at: null, available: true, message: ref('call mum'), ...over
})
const scheduled = (id: number, over: Record<string, unknown> = {}) => ({
  id: fixtureId(id), channel_id: fixtureId(2), channel_name: 'general', dm: false, parent_id: null, reply_to_id: null, content: 'good morning',
  send_at: FIXTURE_TIMESTAMP, state: 'pending', failure_reason: '', created_at: FIXTURE_TIMESTAMP, updated_at: FIXTURE_TIMESTAMP, ...over
})

beforeEach(() => {
  setLocale('en')
  setActivePinia(createPinia())
  apiMock.mockReset()
  vi.spyOn(console, 'warn').mockImplementation(() => undefined)
})

describe('reminders and scheduled messages store', () => {
  it('loads only the lists of the features the server has', async () => {
    apiMock.mockResolvedValueOnce({ items: [reminder(1)] })
    const later = useLaterStore()
    await later.load({ reminders: true, scheduled: false })
    expect(apiMock).toHaveBeenCalledTimes(1)
    expect(apiMock).toHaveBeenCalledWith('/api/me/reminders', expect.anything())
    expect(later.reminders).toHaveLength(1)
    expect(later.loaded).toBe(true)
  })

  it('counts delivered reminders and refused messages as needing attention', async () => {
    apiMock.mockResolvedValueOnce({ items: [reminder(1), reminder(2, { delivered_at: FIXTURE_TIMESTAMP })] })
    apiMock.mockResolvedValueOnce({ items: [scheduled(3), scheduled(4, { state: 'failed', failure_reason: 'slow_mode' })] })
    const later = useLaterStore()
    await later.load({ reminders: true, scheduled: true })
    expect([later.dueCount, later.failedCount, later.attention]).toEqual([1, 1, 2])
  })

  it('sends the absolute time to the server', async () => {
    const at = new Date('2026-10-25T08:00:00Z')
    apiMock.mockResolvedValueOnce(reminder(1))
    const later = useLaterStore()
    await later.remind(fixtureId(40), at)
    expect(apiMock).toHaveBeenLastCalledWith('/api/me/reminders', expect.objectContaining({
      method: 'POST', json: { message_id: fixtureId(40), remind_at: '2026-10-25T08:00:00.000Z' }
    }))
    apiMock.mockResolvedValueOnce(scheduled(2))
    await later.schedule(fixtureId(2), 'hello', at, { replyToId: fixtureId(7) })
    expect(apiMock).toHaveBeenLastCalledWith(`/api/channels/${fixtureId(2)}/scheduled`, expect.objectContaining({
      json: { content: 'hello', send_at: '2026-10-25T08:00:00.000Z', reply_to_id: fixtureId(7) }
    }))
    expect(later.reminders).toHaveLength(1)
    expect(later.scheduled).toHaveLength(1)
  })

  it('edits, cancels and deletes entries', async () => {
    apiMock.mockResolvedValueOnce({ items: [reminder(1)] })
    apiMock.mockResolvedValueOnce({ items: [scheduled(2, { state: 'failed' })] })
    const later = useLaterStore()
    await later.load({ reminders: true, scheduled: true })
    apiMock.mockResolvedValueOnce(scheduled(2, { content: 'edited' }))
    await later.updateScheduled(fixtureId(2), { content: 'edited' })
    expect(apiMock).toHaveBeenLastCalledWith(`/api/me/scheduled/${fixtureId(2)}`, expect.objectContaining({ method: 'PATCH', json: { content: 'edited' } }))
    expect(later.scheduled[0]?.content).toBe('edited')
    apiMock.mockResolvedValue(null)
    await later.removeScheduled(fixtureId(2))
    await later.cancelReminder(fixtureId(1))
    expect(later.scheduled).toEqual([])
    expect(later.reminders).toEqual([])
  })

  it('says what a due reminder is about, and tells of a refused scheduled message', async () => {
    const toasts = useToastStore()
    apiMock.mockResolvedValueOnce({ items: [reminder(1)] })
    const later = useLaterStore()
    await later.load({ reminders: true, scheduled: false })
    apiMock.mockResolvedValueOnce({ items: [reminder(1, { delivered_at: FIXTURE_TIMESTAMP })] })
    await later.onReminderEvent({ id: fixtureId(1), state: 'due' })
    expect(toasts.toasts.at(-1)).toMatchObject({ type: 'info', text: t('later.reminderDueWith', { text: 'call mum' }) })
    expect(later.dueCount).toBe(1)

    apiMock.mockResolvedValueOnce({ items: [] })
    await later.onScheduledEvent({ id: fixtureId(2), channel_id: fixtureId(2), state: 'failed', reason: 'not_allowed' })
    expect(toasts.toasts.at(-1)).toMatchObject({ type: 'error', text: t('later.scheduledFailed'), detail: t('later.reason.not_allowed') })
    // An unknown reason is shown as a plain refusal, never as raw text.
    await later.onScheduledEvent({ id: fixtureId(2), channel_id: fixtureId(2), state: 'failed', reason: '<script>' })
    expect(toasts.toasts.at(-1)?.detail).toBe(t('later.reason.refused'))
  })

  it('clears its lists when the session ends', async () => {
    apiMock.mockResolvedValueOnce({ items: [reminder(1)] })
    const later = useLaterStore()
    await later.load({ reminders: true, scheduled: false })
    later.reset()
    expect(later.reminders).toEqual([])
    expect(later.loaded).toBe(false)
  })

  it('reports a failed load without keeping stale data as loaded', async () => {
    apiMock.mockRejectedValueOnce(new Error('offline'))
    const later = useLaterStore()
    await later.load({ reminders: true, scheduled: true })
    expect(later.failed).toBe(true)
    expect(later.loaded).toBe(false)
  })
})
