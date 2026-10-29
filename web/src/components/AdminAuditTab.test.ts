import type { ApiOptions } from '../lib/api'

import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import AdminAuditTab from './AdminAuditTab.vue'
import { setLocale } from '../i18n'
import { useToastStore } from '../stores/toast'
import { useChatStore } from '../stores/chat'
import { userFixture } from '../test-fixtures.fixture'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args),
  onUnauthorized: vi.fn(),
  ApiError: class ApiError extends Error {}
}))

const entry = (id: number, action: string, extra: Record<string, unknown> = {}) => ({
  id, created_at: '2026-01-01T10:00:00Z', actor_id: '00000000-0000-4000-8000-000000000001', actor_name: 'Mnema',
  action, target_type: 'role', target_id: 'r1', target_name: 'Mods', details: { changes: { name: { from: 'a', to: 'b' } } }, request_id: 'req-1', ...extra
})
const page = (entries: unknown[], next_before: number | null = null) => ({ entries, next_before })

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
})

describe('AdminAuditTab', () => {
  it('lists entries with translated and raw action names, and expands details', async () => {
    apiMock.mockResolvedValue(page([entry(2, 'role.update'), entry(1, 'future.thing', { actor_id: null, actor_name: '', target_name: '', target_id: '' })]))
    const w = mount(AdminAuditTab)
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith('/api/admin/audit-log?limit=50', { decode: expect.any(Function) })
    expect(w.text()).toContain('Role edited')
    expect(w.text()).toContain('future.thing')
    expect(w.text()).toContain('System')
    expect(w.find('pre').exists()).toBe(false)
    await w.find('[data-testid="audit-entry-2"] button').trigger('click')
    expect(w.find('pre').text()).toContain('"from": "a"')
    expect(w.find('[data-testid="audit-summary-2"]').text()).toBe('Changed: name: a → b')
    expect(w.text()).toContain('req-1')
    await w.find('[data-testid="audit-entry-2"] button').trigger('click')
    expect(w.find('pre').exists()).toBe(false)
  })

  it('names the 1.0 events and filters by emoji and bots', async () => {
    apiMock.mockResolvedValue(page([entry(3, 'emoji.create'), entry(2, 'bot.token_regenerate'), entry(1, 'auth.two_factor_enabled')]))
    const w = mount(AdminAuditTab)
    await flushPromises()
    expect(w.text()).toContain('Emoji added')
    expect(w.text()).toContain('Bot token regenerated')
    expect(w.text()).toContain('Two-factor sign-in turned on')
    const options = w.findAll('select')[0]!.findAll('option').map(o => o.text())
    expect(options).toContain('Emoji')
    expect(options).toContain('Bots')
    await w.findAll('select')[0]!.setValue('bot')
    await w.find('[data-testid="audit-filters"]').trigger('submit')
    await flushPromises()
    expect(apiMock).toHaveBeenLastCalledWith('/api/admin/audit-log?limit=50&action=bot', { decode: expect.any(Function) })
  })

  it('filters and pages', async () => {
    apiMock.mockResolvedValueOnce(page([entry(5, 'role.create')], 5))
    const w = mount(AdminAuditTab)
    await flushPromises()
    apiMock.mockResolvedValueOnce(page([entry(4, 'role.delete')]))
    await w.findAll('button').find(b => b.text() === 'Load more')!.trigger('click')
    await flushPromises()
    expect(apiMock).toHaveBeenLastCalledWith('/api/admin/audit-log?limit=50&before=5', { decode: expect.any(Function) })
    expect(w.findAll('[data-testid^="audit-entry-"]')).toHaveLength(2)
    expect(w.text()).not.toContain('Load more')

    const [group, actor] = w.findAll('select')
    await group!.setValue('role')
    await actor!.setValue('')
    const [from, to] = w.findAll<HTMLInputElement>('input[type="date"]')
    await from!.setValue('2026-01-01')
    await to!.setValue('2026-01-31')
    apiMock.mockResolvedValueOnce(page([]))
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(apiMock).toHaveBeenLastCalledWith('/api/admin/audit-log?limit=50&action=role&from=2026-01-01&to=2026-01-31', { decode: expect.any(Function) })
    expect(w.text()).toContain('No entries.')
  })

  it('filters by member', async () => {
    apiMock.mockResolvedValue(page([entry(7, 'role.create', { target_name: '' })]))
    useChatStore().members = [
      { ...userFixture({ id: 'u1', username: 'alice', display_name: '' }), presence: 'online' },
      { ...userFixture({ id: 'u2', username: 'bob', display_name: 'Bobby' }), presence: 'online' }
    ] as never
    const w = mount(AdminAuditTab)
    await flushPromises()
    expect(w.text()).toContain('alice')
    expect(w.text()).toContain('Bobby')
    expect(w.text()).toContain('r1')
    const select = w.findAll('select')[1]!
    await select.setValue('u1')
    await w.find('form').trigger('submit')
    await flushPromises()
    expect(apiMock).toHaveBeenLastCalledWith('/api/admin/audit-log?limit=50&actor=u1', { decode: expect.any(Function) })
  })

  it('reports a failed load and ignores a second load while one runs', async () => {
    apiMock.mockRejectedValueOnce(new Error('boom'))
    const w = mount(AdminAuditTab)
    await flushPromises()
    expect(useToastStore().toasts.map(t => t.text)).toContain('boom')
    let release: (v: unknown) => void = () => {}
    apiMock.mockReturnValueOnce(new Promise(r => { release = r }))
    await w.find('form').trigger('submit')
    await w.find('form').trigger('submit')
    expect(apiMock).toHaveBeenCalledTimes(2)
    release(page([]))
    await flushPromises()
  })

  it('hides sign-ins until asked, and says how many are hidden', async () => {
    apiMock.mockResolvedValue(page([entry(2, 'auth.login', { target_name: '' }), entry(1, 'role.update')]))
    const w = mount(AdminAuditTab)
    await flushPromises()
    expect(w.find('[data-testid="audit-entry-2"]').exists()).toBe(false)
    expect(w.text()).toContain('1 sign-ins hidden')
    await w.find('[data-testid="audit-hide-signins"]').setValue(false)
    expect(w.find('[data-testid="audit-entry-2"]').exists()).toBe(true)
  })

  it('names a report by its reason and a member by name, and never prints an id in the row', async () => {
    const id = '6bac9b37-9f09-4033-891f-c06f798cf12d'
    useChatStore().members = [{ ...userFixture({ id: 'u1', username: 'jonas', display_name: '' }), presence: 'online' }] as never
    apiMock.mockResolvedValue(page([
      entry(3, 'report.create', { target_type: 'report', target_id: id, target_name: '', details: { category: 'spam' } }),
      entry(2, 'user.disable', { target_type: 'user', target_id: 'u1', target_name: '' }),
      entry(1, 'role.update', { target_type: 'invite', target_id: id, target_name: '' })
    ]))
    const w = mount(AdminAuditTab)
    await flushPromises()
    const row = (n: number) => w.find(`[data-testid="audit-entry-${n}"] button`).text()
    expect(row(3)).toContain('Report filed')
    expect(row(3)).toContain('Report (Spam)')
    expect(row(2)).toContain('jonas')
    expect(row(1)).not.toContain(id)
    await w.find('[data-testid="audit-entry-3"] button').trigger('click')
    expect(w.text()).toContain(id)
  })
})
