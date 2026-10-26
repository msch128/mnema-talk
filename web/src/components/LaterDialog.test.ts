import type { ApiOptions } from '../lib/api'

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import LaterDialog from './LaterDialog.vue'
import { closeLater, openLater, pendingLater } from '../lib/laterDialog'
import { useToastStore } from '../stores/toast'
import { setLocale } from '../i18n'
import { fixtureId, FIXTURE_TIMESTAMP } from '../test-fixtures.fixture'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async importOriginal => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args)
}))

enableAutoUnmount(afterEach)

const NOW = new Date(2026, 9, 24, 21, 30, 0)
const reminderReply = { id: fixtureId(1), remind_at: FIXTURE_TIMESTAMP, created_at: FIXTURE_TIMESTAMP, delivered_at: null, available: true }
const scheduledReply = {
  id: fixtureId(2), channel_id: fixtureId(5), channel_name: 'general', dm: false, parent_id: null, reply_to_id: null, content: 'hi',
  send_at: FIXTURE_TIMESTAMP, state: 'pending', failure_reason: '', created_at: FIXTURE_TIMESTAMP, updated_at: FIXTURE_TIMESTAMP
}

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(NOW)
  setLocale('en')
  setActivePinia(createPinia())
  apiMock.mockReset()
  closeLater()
})
afterEach(() => { vi.useRealTimers() })

const mountDialog = () => mount(LaterDialog, { global: { directives: { tooltip: () => undefined } }, attachTo: document.body })

describe('the Remind me dialog', () => {
  it('offers the presets and sets a reminder at the absolute time of the chosen one', async () => {
    apiMock.mockResolvedValueOnce(reminderReply)
    const w = mountDialog()
    expect(w.find('[data-testid="later-dialog"]').exists()).toBe(false)
    openLater({ kind: 'remind', messageId: fixtureId(40), excerpt: 'call mum' })
    await flushPromises()
    expect(w.find('[data-testid="later-excerpt"]').text()).toBe('call mum')
    expect(['in20m', 'in1h', 'in3h', 'tomorrow', 'custom'].every(p => w.find(`[data-testid="later-preset-${p}"]`).exists())).toBe(true)

    await w.find('[data-testid="later-preset-tomorrow"]').setValue(true)
    await w.find('[data-testid="later-dialog"]').trigger('submit')
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith('/api/me/reminders', expect.objectContaining({
      method: 'POST', json: { message_id: fixtureId(40), remind_at: new Date(2026, 9, 25, 8, 0).toISOString() }
    }))
    expect(pendingLater.value).toBeNull()
    expect(useToastStore().toasts.at(-1)?.type).toBe('success')
  })

  it('takes a custom date and time and refuses one in the past without asking the server', async () => {
    apiMock.mockResolvedValueOnce(reminderReply)
    const w = mountDialog()
    openLater({ kind: 'remind', messageId: fixtureId(40), excerpt: 'x' })
    await flushPromises()
    await w.find('[data-testid="later-preset-custom"]').setValue(true)
    const input = w.find('[data-testid="later-custom-input"]')
    expect(w.find('[data-testid="later-confirm"]').attributes('disabled')).toBeDefined()

    await input.setValue('2026-10-24T09:00')
    await w.find('[data-testid="later-dialog"]').trigger('submit')
    await flushPromises()
    expect(apiMock).not.toHaveBeenCalled()
    expect(w.find('[data-testid="later-error"]').exists()).toBe(true)

    await input.setValue('2026-12-24T18:45')
    await w.find('[data-testid="later-dialog"]').trigger('submit')
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith('/api/me/reminders', expect.objectContaining({ json: expect.objectContaining({ remind_at: new Date(2026, 11, 24, 18, 45).toISOString() }) }))
  })

  it('keeps the dialog open and shows the server’s words when it refuses', async () => {
    apiMock.mockRejectedValueOnce(new Error('you can have at most 100 reminders; remove one first'))
    const w = mountDialog()
    openLater({ kind: 'remind', messageId: fixtureId(40), excerpt: 'x' })
    await flushPromises()
    await w.find('[data-testid="later-dialog"]').trigger('submit')
    await flushPromises()
    expect(w.find('[data-testid="later-error"]').text()).toContain('at most 100 reminders')
    expect(pendingLater.value).not.toBeNull()
  })
})

describe('the Send later dialog', () => {
  it('schedules the composed text with its reply target and lets the composer empty itself', async () => {
    apiMock.mockResolvedValueOnce(scheduledReply)
    const onScheduled = vi.fn()
    const w = mountDialog()
    openLater({ kind: 'schedule', channelId: fixtureId(5), content: 'good morning team', replyToId: fixtureId(9), onScheduled })
    await flushPromises()
    expect(w.find('[data-testid="later-excerpt"]').text()).toBe('good morning team')
    expect(w.find('[data-testid="later-preset-in20m"]').exists()).toBe(false)
    await w.find('[data-testid="later-preset-in3h"]').setValue(true)
    await w.find('[data-testid="later-dialog"]').trigger('submit')
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith(`/api/channels/${fixtureId(5)}/scheduled`, expect.objectContaining({
      method: 'POST', json: { content: 'good morning team', send_at: new Date(NOW.getTime() + 3 * 3_600_000).toISOString(), reply_to_id: fixtureId(9) }
    }))
    expect(onScheduled).toHaveBeenCalledTimes(1)
    expect(pendingLater.value).toBeNull()
  })

  it('does not empty the composer when scheduling fails', async () => {
    apiMock.mockRejectedValueOnce(new Error('too many'))
    const onScheduled = vi.fn()
    const w = mountDialog()
    openLater({ kind: 'schedule', channelId: fixtureId(5), content: 'text', onScheduled })
    await flushPromises()
    await w.find('[data-testid="later-dialog"]').trigger('submit')
    await flushPromises()
    expect(onScheduled).not.toHaveBeenCalled()
    expect(w.find('[data-testid="later-error"]').exists()).toBe(true)
  })
})
