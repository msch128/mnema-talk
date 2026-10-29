import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import type { ApiOptions } from '../lib/api'
import BotDetailPanel from './BotDetailPanel.vue'
import { useToastStore } from '../stores/toast'
import { usePermissionsStore } from '../stores/permissions'
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

  it('keeps Save off until something really changed, ignoring spacing in the name', async () => {
    const w = await mountPanel()
    const save = () => w.get('[data-testid="unsaved-save"]').attributes('disabled')
    expect(save()).toBeDefined()
    await w.get('[data-testid="bot-name"]').setValue('  Echo ')
    expect(save()).toBeDefined()
    await w.get('[data-testid="bot-name"]').setValue('Ping')
    expect(save()).toBeUndefined()
    await w.get('[data-testid="bot-name"]').setValue('Echo')
    expect(save()).toBeDefined()
  })

  it('stores nothing until Save: name, switch, intents and roles go together', async () => {
    usePermissionsStore().roles = [{ id: fixtureId(21), name: 'Role 1', color: 0, position: 1, permissions: 0, hoist: false, mentionable: false, is_everyone: false, max_upload_mb: null }]
    const w = await mountPanel(bot({ disabled: true }))
    expect(w.text()).toContain('A disabled bot is rejected at once')
    await w.get('[data-testid="bot-name"]').setValue('Ping')
    await w.get('[data-testid="bot-enabled"]').setValue(true)
    await w.get('[data-intent="members"]').setValue(true)
    await w.get('[data-intent="messages"]').setValue(false)
    await w.get('[data-role="Role 1"] input').setValue(true)
    expect(apiMock.mock.calls.some(([, o]) => o?.method === 'PATCH' || o?.method === 'PUT')).toBe(false)
    expect(w.get('[data-testid="unsaved-status"]').text()).toBe('You have unsaved changes')
    await w.get('[data-testid="unsaved-save"]').trigger('click')
    await flushPromises()
    expect(patches()).toEqual([{ name: 'Ping', disabled: false, allowed_intents: 2 }])
    expect(apiMock).toHaveBeenCalledWith(`${base}/roles`, expect.objectContaining({ method: 'PUT', json: { role_ids: [fixtureId(21)] } }))
    expect(w.emitted('updated')).toHaveLength(2)
    expect(toastTexts()).toContain('Bot saved.')
  })

  it('refuses an empty name inline', async () => {
    const w = await mountPanel()
    await w.get('[data-testid="bot-name"]').setValue('  ')
    expect(w.get('[data-testid="bot-name-error"]').text()).toBe('A bot needs a name.')
    expect(w.get('[data-testid="unsaved-save"]').attributes('disabled')).toBeDefined()
  })

  it('follows a rename from elsewhere, and reset brings the saved state back', async () => {
    const w = await mountPanel()
    await w.setProps({ bot: bot({ name: 'Renamed' }) })
    expect((w.get('[data-testid="bot-name"]').element as HTMLInputElement).value).toBe('Renamed')
    await w.get('[data-testid="bot-name"]').setValue('Other')
    await w.get('[data-testid="unsaved-reset"]').trigger('click')
    expect((w.get('[data-testid="bot-name"]').element as HTMLInputElement).value).toBe('Renamed')
  })

  it('shows a failed save inline and keeps the edits; after a partial save only the roles stay unsaved', async () => {
    usePermissionsStore().roles = [{ id: fixtureId(21), name: 'Role 1', color: 0, position: 1, permissions: 0, hoist: false, mentionable: false, is_everyone: false, max_upload_mb: null }]
    const w = await mountPanel()
    apiMock.mockImplementation(async (path, options) => {
      if (path === `${base}/roles`) throw new Error('only the Owner may assign this role')
      if (path === base && options?.method === 'PATCH') return bot({ ...(options.json as object) })
      return []
    })
    await w.get('[data-testid="bot-name"]').setValue('Ping')
    await w.get('[data-role="Role 1"] input').setValue(true)
    await w.get('[data-testid="unsaved-save"]').trigger('click')
    await flushPromises()
    expect(w.get('[data-testid="unsaved-error"]').text()).toContain('only the Owner may assign this role')
    expect(w.get('[data-testid="unsaved-error"]').text()).toContain('Some changes were saved')
    apiMock.mockClear()
    await w.get('[data-testid="unsaved-save"]').trigger('click')
    await flushPromises()
    expect(patches()).toEqual([])
  })

  it('offers the discard guard to its parent', async () => {
    const w = await mountPanel()
    const guard = (w.vm as unknown as { guard: () => Promise<boolean> }).guard
    expect(await guard()).toBe(true)
    await w.get('[data-testid="bot-name"]').setValue('Ping')
    confirmMock.mockResolvedValueOnce(false)
    expect(await guard()).toBe(false)
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

  it('reloads the access preview after the roles were saved', async () => {
    usePermissionsStore().roles = [{ id: fixtureId(21), name: 'Role 1', color: 0, position: 1, permissions: 0, hoist: false, mentionable: false, is_everyone: false, max_upload_mb: null }]
    apiMock.mockImplementation(async (path, options) => {
      if (path === `${base}/roles`) return bot({ role_ids: [fixtureId(21)] })
      if (path === base && options?.method === 'PATCH') return bot()
      if (path.endsWith('/access')) return { permissions: 0, elevated: false, channels: [] }
      return []
    })
    const w = await mountPanel()
    const before = apiMock.mock.calls.filter(([p]) => p.endsWith('/access')).length
    await w.get('[data-role="Role 1"] input').setValue(true)
    await w.get('[data-testid="unsaved-save"]').trigger('click')
    await flushPromises()
    expect(apiMock.mock.calls.filter(([p]) => p.endsWith('/access')).length).toBe(before + 1)
  })
})
