import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import type { ApiOptions } from '../lib/api'
import BotCommandsPanel from './BotCommandsPanel.vue'
import { useToastStore } from '../stores/toast'
import { fixtureId } from '../test-fixtures.fixture'
import { setLocale } from '../i18n'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args),
  onUnauthorized: vi.fn()
}))
const confirmMock = vi.fn<() => Promise<boolean>>()
vi.mock('../lib/confirm', () => ({ confirm: () => confirmMock() }))

const botId = fixtureId(5)
const base = `/api/admin/bots/${botId}/commands`
const command = (n: number, extra: Record<string, unknown> = {}) => ({
  id: fixtureId(40 + n), bot_id: botId, name: `cmd${n}`, description: `Does ${n}`, options: [], disabled: false, created_at: '2026-01-02T03:04:05Z', ...extra
})
const toastTexts = () => useToastStore().toasts.map(t => t.text)

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
  confirmMock.mockReset()
})

const mountPanel = async (list: unknown[] = [command(1, { options: [{}, {}] }), command(2, { disabled: true })]) => {
  apiMock.mockResolvedValueOnce(list)
  const w = mount(BotCommandsPanel, { props: { botId } })
  await flushPromises()
  return w
}

describe('BotCommandsPanel', () => {
  it('lists commands with option counts and the visibility note', async () => {
    const w = await mountPanel()
    const rows = w.findAll('[data-testid="bot-command"]')
    expect(rows).toHaveLength(2)
    expect(rows[0]?.text()).toContain('/cmd1')
    expect(rows[0]?.text()).toContain('2 options')
    expect(rows[1]?.text()).toContain('Disabled')
    expect(w.text()).toContain('where the bot can view and send')
  })

  it('shows the empty state', async () => {
    expect((await mountPanel([])).text()).toContain('has not registered any commands')
  })

  it('switches a command off and on', async () => {
    const w = await mountPanel()
    apiMock.mockResolvedValueOnce(command(1, { disabled: true }))
    await w.findAll('input[type="checkbox"]')[0]?.setValue(false)
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith(`${base}/${fixtureId(41)}`, expect.objectContaining({ method: 'PATCH', json: { disabled: true } }))
    expect(w.findAll('[data-testid="bot-command"]')[0]?.text()).toContain('Disabled')
  })

  it('reloads and reports when switching fails', async () => {
    const w = await mountPanel()
    apiMock.mockRejectedValueOnce(new Error('patch failed'))
    apiMock.mockResolvedValueOnce([command(1)])
    await w.findAll('input[type="checkbox"]')[0]?.setValue(false)
    await flushPromises()
    expect(toastTexts()).toContain('patch failed')
    expect((w.findAll('input[type="checkbox"]')[0]!.element as HTMLInputElement).checked).toBe(true)
    expect(w.findAll('[data-testid="bot-command"]')).toHaveLength(1)
  })

  it('deletes after confirming', async () => {
    const w = await mountPanel()
    confirmMock.mockResolvedValueOnce(false)
    await w.findAll('[data-action="delete"]')[0]?.trigger('click')
    await flushPromises()
    expect(w.findAll('[data-testid="bot-command"]')).toHaveLength(2)
    confirmMock.mockResolvedValueOnce(true)
    apiMock.mockResolvedValueOnce(undefined)
    await w.findAll('[data-action="delete"]')[0]?.trigger('click')
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith(`${base}/${fixtureId(41)}`, { method: 'DELETE' })
    expect(w.findAll('[data-testid="bot-command"]')).toHaveLength(1)
  })

  it('reports delete and load failures', async () => {
    const w = await mountPanel()
    confirmMock.mockResolvedValueOnce(true)
    apiMock.mockRejectedValueOnce(new Error('delete failed'))
    await w.findAll('[data-action="delete"]')[0]?.trigger('click')
    await flushPromises()
    expect(toastTexts()).toContain('delete failed')
    apiMock.mockRejectedValueOnce(new Error('load failed'))
    mount(BotCommandsPanel, { props: { botId } })
    await flushPromises()
    expect(toastTexts()).toContain('load failed')
  })
})
