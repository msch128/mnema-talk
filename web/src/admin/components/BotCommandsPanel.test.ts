import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import type { ApiOptions } from '../../shared/lib/api'
import BotCommandsPanel from './BotCommandsPanel.vue'
import { useToastStore } from '../../stores/toast'
import { fixtureId } from '../../test-fixtures.fixture'
import { setLocale } from '../../i18n'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../../shared/lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../../shared/lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args),
  onUnauthorized: vi.fn()
}))
const confirmMock = vi.fn<() => Promise<boolean>>()
vi.mock('../../shared/lib/confirm', () => ({ confirm: () => confirmMock() }))

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

  it('turns a command off and on with a button and confirms each', async () => {
    const w = await mountPanel()
    const first = () => w.findAll('[data-testid="bot-command"]')[0]!
    expect(w.find('input[type="checkbox"]').exists()).toBe(false)
    expect(first().get('[data-action="toggle"]').text()).toBe('Turn off')
    apiMock.mockResolvedValueOnce(command(1, { disabled: true }))
    await first().get('[data-action="toggle"]').trigger('click')
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith(`${base}/${fixtureId(41)}`, expect.objectContaining({ method: 'PATCH', json: { disabled: true } }))
    expect(first().get('[data-testid="bot-command-state"]').text()).toBe('Disabled')
    expect(first().get('[data-action="toggle"]').text()).toBe('Turn on')
    apiMock.mockResolvedValueOnce(command(1))
    await first().get('[data-action="toggle"]').trigger('click')
    await flushPromises()
    expect(apiMock).toHaveBeenLastCalledWith(`${base}/${fixtureId(41)}`, expect.objectContaining({ json: { disabled: false } }))
    expect(toastTexts()).toEqual(['/cmd1 is off.', '/cmd1 is on.'])
  })

  it('reports a refused change and keeps the state', async () => {
    const w = await mountPanel()
    apiMock.mockRejectedValueOnce(new Error('patch failed'))
    await w.findAll('[data-action="toggle"]')[0]?.trigger('click')
    await flushPromises()
    expect(toastTexts()).toEqual(['patch failed'])
    expect(w.findAll('[data-testid="bot-command-state"]')[0]?.text()).toBe('Enabled')
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
    expect(toastTexts()).toEqual(['/cmd1 deleted.'])
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
