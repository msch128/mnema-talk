import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import type { ApiOptions } from '../lib/api'
import BotTokensPanel from './BotTokensPanel.vue'
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
const base = `/api/admin/bots/${botId}/tokens`
const stamp = '2026-01-02T03:04:05Z'
const token = (n: number, extra: Record<string, unknown> = {}) => ({ id: fixtureId(30 + n), name: `T${n}`, hint: `ab${n}c`, state: 'active', created_at: stamp, ...extra })
const toastTexts = () => useToastStore().toasts.map(t => t.text)

let tokens: unknown[]
beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
  confirmMock.mockReset()
  tokens = [token(1, { last_used_at: stamp, expires_at: stamp }), token(2, { name: '', state: 'revoked' })]
  apiMock.mockImplementation(async (path, options) => {
    if (path === base && !options?.method) return tokens
    if (path === base && options?.method === 'POST') return { ...token(3), secret: 'mb_new' }
    if (path.endsWith('/regenerate')) return { ...token(1), secret: 'mb_regen' }
    return undefined
  })
})

const mountPanel = async () => {
  const w = mount(BotTokensPanel, { props: { botId, botName: 'Echo' } })
  await flushPromises()
  return w
}

describe('BotTokensPanel', () => {
  it('lists tokens with hint, state and usage', async () => {
    const w = await mountPanel()
    const rows = w.findAll('[data-testid="bot-token"]')
    expect(rows).toHaveLength(2)
    expect(rows[0]?.text()).toContain('...ab1c')
    expect(rows[0]?.text()).toContain('Active')
    expect(rows[1]?.text()).toContain('Revoked')
    expect(rows[1]?.text()).toContain('Never')
    expect(rows[1]?.get('[data-action="revoke"]').attributes('disabled')).toBeDefined()
  })

  it('marks pre-1.0 tokens that have no recorded issuer, and tells which binding was assumed', async () => {
    tokens = [
      token(1),
      token(2, { issuer_binding: 'unbound' }),
      token(3, { issuer_binding: 'inferred' })
    ]
    const w = await mountPanel()
    const badges = w.findAll('[data-testid="bot-token-binding"]')
    expect(badges.map(b => b.text())).toEqual(['Unbound', 'Issuer assumed'])
    expect(badges[0]?.attributes('title')).toContain('no issuer is recorded')
    expect(badges[1]?.attributes('title')).toContain('stops when that person loses the right')
    expect(w.findAll('[data-testid="bot-token"]')[0]?.find('[data-testid="bot-token-binding"]').exists()).toBe(false)
  })

  it('shows the empty state', async () => {
    tokens = []
    const w = await mountPanel()
    expect(w.text()).toContain('No tokens yet.')
  })

  it('issues a token with name and expiry and shows the secret once', async () => {
    const w = await mountPanel()
    await w.get('input[type="text"]').setValue(' prod ')
    await w.get('input[type="date"]').setValue('2030-05-06')
    await w.get('form').trigger('submit')
    await flushPromises()
    const call = apiMock.mock.calls.find(([, o]) => o?.method === 'POST')
    expect(call?.[1]?.json).toEqual({ name: 'prod', expires_at: expect.stringMatching(/^2030-05-0[67]T/) })
    expect(w.get('[data-testid="bot-secret-value"]').text()).toBe('mb_new')
    await w.get('[data-testid="bot-secret-done"]').trigger('click')
    expect(w.find('[data-testid="bot-secret"]').exists()).toBe(false)
  })

  it('issues without options', async () => {
    const w = await mountPanel()
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(apiMock.mock.calls.find(([, o]) => o?.method === 'POST')?.[1]?.json).toEqual({})
  })

  it('reports an issue failure', async () => {
    const w = await mountPanel()
    apiMock.mockRejectedValueOnce(new Error('no rights'))
    await w.get('form').trigger('submit')
    await flushPromises()
    expect(toastTexts()).toContain('no rights')
  })

  it('regenerates after confirming and reveals the new secret', async () => {
    const w = await mountPanel()
    confirmMock.mockResolvedValueOnce(false)
    await w.get('[data-action="regenerate"]').trigger('click')
    await flushPromises()
    expect(w.find('[data-testid="bot-secret"]').exists()).toBe(false)
    confirmMock.mockResolvedValueOnce(true)
    await w.get('[data-action="regenerate"]').trigger('click')
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith(`${base}/${fixtureId(31)}/regenerate`, expect.objectContaining({ method: 'POST' }))
    expect(w.get('[data-testid="bot-secret-value"]').text()).toBe('mb_regen')
  })

  it('reports a regenerate failure', async () => {
    const w = await mountPanel()
    confirmMock.mockResolvedValueOnce(true)
    apiMock.mockRejectedValueOnce(new Error('nope'))
    await w.get('[data-action="regenerate"]').trigger('click')
    await flushPromises()
    expect(toastTexts()).toContain('nope')
  })

  it('revokes after confirming', async () => {
    const w = await mountPanel()
    confirmMock.mockResolvedValueOnce(false)
    await w.get('[data-action="revoke"]').trigger('click')
    await flushPromises()
    expect(apiMock.mock.calls.some(([, o]) => o?.method === 'DELETE')).toBe(false)
    confirmMock.mockResolvedValueOnce(true)
    await w.get('[data-action="revoke"]').trigger('click')
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith(`${base}/${fixtureId(31)}`, { method: 'DELETE' })
  })

  it('reports a revoke failure and a load failure', async () => {
    const w = await mountPanel()
    confirmMock.mockResolvedValueOnce(true)
    apiMock.mockRejectedValueOnce(new Error('revoke failed'))
    await w.get('[data-action="revoke"]').trigger('click')
    await flushPromises()
    expect(toastTexts()).toContain('revoke failed')
    apiMock.mockReset()
    apiMock.mockRejectedValue(new Error('load failed'))
    await mountPanel()
    expect(toastTexts()).toContain('load failed')
  })
})
