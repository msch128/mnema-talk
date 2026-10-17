import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import type { ApiOptions } from '../lib/api'
import BotDetailPanel from './BotDetailPanel.vue'
import { useToastStore } from '../stores/toast'
import { fixtureId } from '../test-fixtures.fixture'
import { setLocale } from '../i18n'
import type { BotBot } from '../types/rest'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args),
  onUnauthorized: vi.fn()
}))
const confirmMock = vi.fn<() => Promise<boolean>>()
vi.mock('../lib/confirm', () => ({ confirm: () => confirmMock() }))

const bot = (extra: Partial<BotBot> = {}): BotBot => ({ id: fixtureId(5), user_id: fixtureId(6), name: 'Echo', allowed_intents: 512, disabled: false, role_ids: [], created_at: '2026-01-02T03:04:05Z', ...extra })
const base = `/api/admin/bots/${fixtureId(5)}`
const toastTexts = () => useToastStore().toasts.map(t => t.text)

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
  confirmMock.mockReset()
  // the child panels load their own data; keep them quiet
  apiMock.mockImplementation(async (path, options) => {
    if (path === base && options?.method === 'PATCH') return bot({ ...(options.json as object) })
    if (path.endsWith('/access')) return { permissions: 0, elevated: false, channels: [] }
    if (path.endsWith('/tokens') || path.endsWith('/commands') || path === '/api/roles') return []
    return undefined
  })
})

const mountPanel = async (b = bot()) => {
  const w = mount(BotDetailPanel, { props: { bot: b } })
  await flushPromises()
  return w
}
const patches = () => apiMock.mock.calls.filter(([, o]) => o?.method === 'PATCH').map(([, o]) => o?.json)

describe('BotDetailPanel', () => {
  it('shows initials without an avatar and the avatar when present', async () => {
    expect((await mountPanel()).text()).toContain('E')
    const w = await mountPanel(bot({ avatar_url: '/api/media/a' }))
    expect(w.get('img').attributes('src')).toBe('/api/media/a')
  })

  it('renames on enter and ignores unchanged or empty names', async () => {
    const w = await mountPanel()
    const input = w.get('[data-testid="bot-name"]')
    await input.setValue('Ping')
    await input.trigger('keydown.enter')
    await flushPromises()
    expect(patches()).toEqual([{ name: 'Ping' }])
    expect(w.emitted('updated')).toHaveLength(1)
    await input.setValue('Echo')
    await input.trigger('blur')
    await input.setValue('  ')
    await input.trigger('blur')
    await flushPromises()
    expect(patches()).toHaveLength(1)
    expect(toastTexts()).toContain('A bot needs a name.')
    expect((input.element as HTMLInputElement).value).toBe('Echo')
  })

  it('syncs the name field with the bot and reverts on a failed rename', async () => {
    const w = await mountPanel()
    await w.setProps({ bot: bot({ name: 'Renamed' }) })
    expect((w.get('[data-testid="bot-name"]').element as HTMLInputElement).value).toBe('Renamed')
    apiMock.mockRejectedValueOnce(new Error('too long'))
    await w.get('[data-testid="bot-name"]').setValue('Other')
    await w.get('[data-testid="bot-name"]').trigger('blur')
    await flushPromises()
    expect(toastTexts()).toContain('too long')
    expect((w.get('[data-testid="bot-name"]').element as HTMLInputElement).value).toBe('Renamed')
  })

  it('toggles enabled and intents', async () => {
    const w = await mountPanel(bot({ disabled: true }))
    expect(w.text()).toContain('A disabled bot is rejected at once')
    await w.get('[data-testid="bot-enabled"]').setValue(true)
    await flushPromises()
    await w.get('[data-intent="members"]').setValue(true)
    await flushPromises()
    await w.get('[data-intent="messages"]').setValue(false)
    await flushPromises()
    expect(patches()).toEqual([{ disabled: false }, { allowed_intents: 514 }, { allowed_intents: 0 }])
  })

  it('ignores a second change while one is being saved', async () => {
    const w = await mountPanel()
    let release: (b: BotBot) => void = () => {}
    apiMock.mockImplementationOnce(() => new Promise(resolve => { release = resolve as (b: BotBot) => void }))
    const input = w.get('[data-testid="bot-name"]')
    await input.setValue('First')
    void input.trigger('blur')
    await input.setValue('Second')
    await input.trigger('blur')
    release(bot({ name: 'First' }))
    await flushPromises()
    expect(patches()).toEqual([{ name: 'First' }])
  })

  it('uploads an avatar and reports failures', async () => {
    const w = await mountPanel()
    const file = new File(['x'], 'a.png', { type: 'image/png' })
    const input = w.get('[data-testid="bot-avatar"]')
    Object.defineProperty(input.element, 'files', { value: [file], configurable: true })
    apiMock.mockResolvedValueOnce(bot({ avatar_url: '/api/media/new' }))
    await input.trigger('change')
    await flushPromises()
    const call = apiMock.mock.calls.find(([p]) => p === `${base}/avatar`)
    expect(call?.[1]?.form?.get('avatar')).toBe(file)
    expect(w.emitted('updated')).toHaveLength(1)
    apiMock.mockRejectedValueOnce(new Error('bad image'))
    Object.defineProperty(input.element, 'files', { value: [file], configurable: true })
    await input.trigger('change')
    await flushPromises()
    expect(toastTexts()).toContain('bad image')
    Object.defineProperty(input.element, 'files', { value: [], configurable: true })
    await input.trigger('change')
    expect(apiMock.mock.calls.filter(([p]) => p === `${base}/avatar`)).toHaveLength(2)
  })

  it('deletes after confirming and reports a failure', async () => {
    const w = await mountPanel()
    confirmMock.mockResolvedValueOnce(false)
    await w.get('[data-testid="bot-delete"]').trigger('click')
    await flushPromises()
    expect(w.emitted('deleted')).toBeUndefined()
    confirmMock.mockResolvedValueOnce(true)
    apiMock.mockRejectedValueOnce(new Error('rank too low'))
    await w.get('[data-testid="bot-delete"]').trigger('click')
    await flushPromises()
    expect(toastTexts()).toContain('rank too low')
    confirmMock.mockResolvedValueOnce(true)
    await w.get('[data-testid="bot-delete"]').trigger('click')
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith(base, { method: 'DELETE' })
    expect(w.emitted('deleted')?.[0]).toEqual([fixtureId(5)])
  })

  it('reloads the access preview when roles change', async () => {
    const w = await mountPanel()
    const before = apiMock.mock.calls.filter(([p]) => p.endsWith('/access')).length
    w.findComponent({ name: 'BotRolesPanel' }).vm.$emit('updated', bot({ role_ids: [fixtureId(21)] }))
    await flushPromises()
    expect(apiMock.mock.calls.filter(([p]) => p.endsWith('/access')).length).toBe(before + 1)
    expect(w.emitted('updated')).toHaveLength(1)
  })
})
