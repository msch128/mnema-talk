import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import AdminMessagesTab from './AdminMessagesTab.vue'
import { pendingConfirm } from '../../shared/lib/confirm'
import { setLocale } from '../../i18n'

// Server settings > Messages: the lifetime switch and ceiling, and the
// community's auto-delete default, which asks with the dry run's counts first.

enableAutoUnmount(afterEach)

let calls: { url: string; method: string; body: unknown }[]
let view: Record<string, unknown>
let preview = { messages: 4, attachments: 2, next_run_at: '2026-11-13T10:10:00Z' }

beforeEach(() => {
  setLocale('en')
  setActivePinia(createPinia())
  calls = []
  view = { message_lifetimes_enabled: true, message_lifetime_max_seconds: 2592000, auto_delete_default_hours: 0, auto_delete_default_scope: 'all', auto_delete_next_run_at: '2026-11-13T10:10:00Z' }
  preview = { messages: 4, attachments: 2, next_run_at: '2026-11-13T10:10:00Z' }
  vi.stubGlobal('fetch', vi.fn<typeof fetch>(async (url, init) => {
    const body = typeof init?.body === 'string' ? JSON.parse(init.body) : undefined
    calls.push({ url: String(url), method: init?.method ?? 'GET', body })
    if (String(url).endsWith('/preview')) return json(preview)
    if (init?.method === 'PUT' && String(url).endsWith('/lifetimes')) view = { ...view, ...body }
    if (init?.method === 'PUT' && String(url).endsWith('/auto-delete')) view = { ...view, auto_delete_default_hours: body.hours, auto_delete_default_scope: body.scope }
    return json(view)
  }))
})
afterEach(() => vi.unstubAllGlobals())

function json(v: unknown) {
  return new Response(JSON.stringify(v), { status: 200, headers: { 'Content-Type': 'application/json' } })
}

async function mountTab() {
  const w = mount(AdminMessagesTab, { attachTo: document.body, global: { directives: { tooltip: () => undefined } } })
  await flushPromises()
  return w
}

const saveIn = (w: Awaited<ReturnType<typeof mountTab>>, panel: string) => w.get(`[data-testid="${panel}"] form`).trigger('submit')

describe('the message settings', () => {
  it('switches lifetimes and sets the longest one with one Save', async () => {
    const w = await mountTab()
    await w.get('[data-testid="lifetimes-enabled"]').setValue(false)
    await w.get('[data-testid="lifetime-max"]').setValue(7)
    await saveIn(w, 'lifetime-settings')
    await flushPromises()
    expect(calls.find(c => c.method === 'PUT')).toEqual({ url: '/api/admin/message-settings/lifetimes', method: 'PUT', body: { message_lifetimes_enabled: false, message_lifetime_max_seconds: 7 * 86400 } })
  })

  it('refuses a longest lifetime beyond 30 days', async () => {
    const w = await mountTab()
    await w.get('[data-testid="lifetime-max"]').setValue(31)
    expect(w.get('[data-testid="lifetime-settings"]').text()).toContain('Choose between 1 minute and 30 days.')
  })

  it('asks with the counts of the dry run before a default removes anything, and saves only when confirmed', async () => {
    const w = await mountTab()
    await w.get('[data-testid="auto-delete-default-on"]').setValue(true)
    await w.get('[data-testid="auto-delete-default-amount"]').setValue(2)
    await w.get('#default-unit').setValue('weeks')
    expect(w.get('[data-testid="auto-delete-default-summary"]').text()).toBe('Channels that follow the default delete messages older than 2 weeks, with their files.')
    await saveIn(w, 'auto-delete-default')
    await flushPromises()
    expect(calls.find(c => c.url.endsWith('/preview'))?.body).toEqual({ hours: 336, scope: 'all' })
    expect(pendingConfirm.value?.body).toContain('4 messages and 2 files')
    pendingConfirm.value?.resolve(false)
    await flushPromises()
    expect(calls.some(c => c.method === 'PUT')).toBe(false)

    await saveIn(w, 'auto-delete-default')
    await flushPromises()
    pendingConfirm.value?.resolve(true)
    await flushPromises()
    expect(calls.find(c => c.method === 'PUT')).toEqual({ url: '/api/admin/message-settings/auto-delete', method: 'PUT', body: { hours: 336, scope: 'all' } })
  })
})
