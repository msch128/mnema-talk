import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import type { ApiOptions } from '../lib/api'
import AdminBotsTab from './AdminBotsTab.vue'
import { usePermissionsStore } from '../stores/permissions'
import { useToastStore } from '../stores/toast'
import { fixtureId } from '../test-fixtures.fixture'
import { setLocale } from '../i18n'
import type { Role } from '../types/domain'
import type { BotBot } from '../types/rest'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args),
  onUnauthorized: vi.fn()
}))
const confirmMock = vi.fn<() => Promise<boolean>>()
vi.mock('../lib/confirm', () => ({ confirm: () => confirmMock() }))

const role: Role = { id: fixtureId(21), name: 'Helper', color: 0, position: 1, permissions: 4, hoist: false, mentionable: false, is_everyone: false, max_upload_mb: null }
const bot = (n: number, extra: Partial<BotBot> = {}): BotBot => ({ id: fixtureId(10 + n), user_id: fixtureId(30 + n), name: `Bot${n}`, allowed_intents: 0, disabled: false, role_ids: [], created_at: '2026-01-02T03:04:05Z', ...extra })
let bots: BotBot[]

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
  confirmMock.mockReset()
  bots = [bot(1, { role_ids: [role.id], last_active_at: new Date(Date.now() - 3 * 3600_000).toISOString() }), bot(2, { disabled: true, avatar_url: '/api/media/a' })]
  apiMock.mockImplementation(async (path, options) => {
    if (path === '/api/roles') return [role]
    if (path === '/api/admin/bots' && !options?.method) return bots
    if (path === '/api/admin/bots' && options?.method === 'POST') return bot(3, { name: (options.json as { name: string }).name })
    if (path.endsWith('/access')) return { permissions: 0, elevated: false, channels: [] }
    if (/\/(tokens|commands)$/.test(path)) return []
    return undefined
  })
})

const mountTab = async () => {
  const w = mount(AdminBotsTab)
  await flushPromises()
  return w
}

describe('AdminBotsTab', () => {
  it('lists bots with badge, state, activity and role chips', async () => {
    const w = await mountTab()
    const rows = w.findAll('[data-testid="bot-row"]')
    expect(rows).toHaveLength(2)
    expect(rows[0]?.text()).toContain('Bot1')
    expect(rows[0]?.text()).toContain('Helper')
    expect(rows[0]?.text()).toContain('3 hours ago')
    expect(rows[1]?.text()).toContain('Disabled')
    expect(rows[1]?.text()).toContain('never active')
    expect(rows[1]?.text()).toContain('No role')
    expect(rows[1]?.get('img').attributes('src')).toBe('/api/media/a')
    expect(w.text()).toContain('Pick a bot to manage it.')
  })

  it('shows the empty state', async () => {
    bots = []
    expect((await mountTab()).text()).toContain('No bots yet.')
  })

  it('creates a bot and opens it', async () => {
    const w = await mountTab()
    await w.get('[data-testid="bot-new-name"]').setValue(' Newbie ')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith('/api/admin/bots', expect.objectContaining({ method: 'POST', json: { name: 'Newbie' } }))
    expect(w.findAll('[data-testid="bot-row"]')).toHaveLength(3)
    expect(w.find('[data-testid="bot-detail"]').exists()).toBe(true)
    expect(useToastStore().toasts.map(t => t.text)).toContain('Bot created. It has no access yet.')
  })

  it('does not submit an empty name and reports a create failure', async () => {
    const w = await mountTab()
    await w.get('form').trigger('submit')
    expect(apiMock.mock.calls.some(([, o]) => o?.method === 'POST')).toBe(false)
    apiMock.mockRejectedValueOnce(new Error('taken'))
    await w.get('[data-testid="bot-new-name"]').setValue('Bot1')
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(useToastStore().toasts.map(t => t.text)).toContain('taken')
  })

  it('selects a bot, applies updates and removes it when deleted', async () => {
    const w = await mountTab()
    await w.findAll('[data-testid="bot-row"] button')[0]?.trigger('click')
    expect(w.find('[data-testid="bot-detail"]').exists()).toBe(true)
    apiMock.mockImplementationOnce(async () => bot(1, { name: 'Renamed' }))
    await w.get('[data-testid="bot-name"]').setValue('Renamed')
    await w.get('[data-testid="unsaved-save"]').trigger('click')
    await flushPromises()
    expect(w.findAll('[data-testid="bot-row"]')[0]?.text()).toContain('Renamed')
    confirmMock.mockResolvedValueOnce(true)
    await w.get('[data-testid="bot-delete"]').trigger('click')
    await flushPromises()
    expect(w.findAll('[data-testid="bot-row"]')).toHaveLength(1)
    expect(w.find('[data-testid="bot-detail"]').exists()).toBe(false)
  })

  it('asks before another bot replaces a detail with unsaved edits', async () => {
    const w = await mountTab()
    const rows = () => w.findAll('[data-testid="bot-row"] button')
    await rows()[0]?.trigger('click')
    await w.get('[data-testid="bot-name"]').setValue('Edited')
    confirmMock.mockResolvedValueOnce(false)
    await rows()[1]?.trigger('click')
    await flushPromises()
    expect((w.get('[data-testid="bot-name"]').element as HTMLInputElement).value).toBe('Edited')
    confirmMock.mockResolvedValueOnce(true)
    await rows()[1]?.trigger('click')
    await flushPromises()
    expect((w.get('[data-testid="bot-name"]').element as HTMLInputElement).value).not.toBe('Edited')
  })

  it('keeps the selection when another bot is deleted and reports load failures', async () => {
    const w = await mountTab()
    await w.findAll('[data-testid="bot-row"] button')[0]?.trigger('click')
    ;(w.findComponent({ name: 'BotDetailPanel' }).vm as unknown as { $emit: (e: string, ...a: unknown[]) => void }).$emit('deleted', fixtureId(99))
    await flushPromises()
    expect(w.find('[data-testid="bot-detail"]').exists()).toBe(true)
    apiMock.mockReset()
    apiMock.mockRejectedValue(new Error('load failed'))
    mount(AdminBotsTab)
    await flushPromises()
    expect(useToastStore().toasts.map(t => t.text)).toContain('load failed')
  })

  it('ignores role ids the store does not know', async () => {
    bots = [bot(1, { role_ids: [fixtureId(99)] })]
    usePermissionsStore()
    const w = await mountTab()
    expect(w.get('[data-testid="bot-row"]').text()).toContain('No role')
  })
})
