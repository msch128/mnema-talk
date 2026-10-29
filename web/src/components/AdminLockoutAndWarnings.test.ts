import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import type { ApiOptions } from '../lib/api'
import type { AdminUser } from '../types/domain'
import AdminUsersTab from './AdminUsersTab.vue'
import AdminSystemTab from './AdminSystemTab.vue'
import { systemStatus } from './systemStatus.fixture'
import { fixtureId, userFixture } from '../test-fixtures.fixture'
import { useAuthStore } from '../stores/auth'
import { useToastStore } from '../stores/toast'
import { setLocale } from '../i18n'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('./AdminOpsPanel.vue', () => ({ default: { template: '<div />' } }))
vi.mock('./AdminUploadLimit.vue', () => ({ default: { template: '<div />' } }))
vi.mock('../lib/api', async (original) => ({
  ...(await original<typeof import('../lib/api')>()),
  api: (path: string, options?: ApiOptions) => apiMock(path, options),
}))

let wrappers: VueWrapper[] = []
function keep<T extends VueWrapper>(w: T): T {
  wrappers.push(w)
  return w
}
function account(locked: boolean): AdminUser {
  return {
    ...userFixture({ id: fixtureId(41), username: 'member', display_name: '' }),
    disabled: false,
    banned: false,
    last_seen_at: null,
    login_locked: locked,
  }
}

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
})
afterEach(() => {
  for (const w of wrappers) w.unmount()
  wrappers = []
  document.body.innerHTML = ''
})

describe('login lockout in the users tab', () => {
  it('shows the indicator and lets the Owner clear the lockout', async () => {
    useAuthStore().user = userFixture({ role: 'admin', username: 'owner' })
    apiMock.mockResolvedValueOnce([account(true)])
    const w = keep(mount(AdminUsersTab))
    await flushPromises()
    expect(w.find('[data-testid="user-login-locked"]').exists()).toBe(true)
    apiMock.mockResolvedValueOnce(undefined).mockResolvedValueOnce([account(false)])
    await w.find('[data-testid="action-clear-lockout"]').trigger('click')
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith(`/api/admin/users/${fixtureId(41)}/login-lockout/clear`, { method: 'POST' })
    expect(useToastStore().toasts.some((t) => t.type === 'success')).toBe(true)
    expect(w.find('[data-testid="user-login-locked"]').exists()).toBe(false)
    expect(w.find('[data-testid="action-clear-lockout"]').exists()).toBe(false)
  })

  it('reports a failed clear', async () => {
    useAuthStore().user = userFixture({ role: 'admin', username: 'owner' })
    apiMock.mockResolvedValueOnce([account(true)])
    const w = keep(mount(AdminUsersTab))
    await flushPromises()
    apiMock.mockRejectedValueOnce(new Error('clear denied'))
    await w.find('[data-testid="action-clear-lockout"]').trigger('click')
    await flushPromises()
    expect(useToastStore().toasts.map((t) => t.text)).toContain('clear denied')
  })

  it('offers no clear action to non-Owners', async () => {
    useAuthStore().user = userFixture({ role: 'user', username: 'mod' })
    apiMock.mockResolvedValueOnce([account(true)])
    const w = keep(mount(AdminUsersTab))
    await flushPromises()
    expect(w.find('[data-testid="action-clear-lockout"]').exists()).toBe(false)
  })
})

describe('operator warnings in the system tab', () => {
  it('shows nothing without warnings', async () => {
    apiMock.mockResolvedValue(systemStatus())
    const w = keep(mount(AdminSystemTab))
    await flushPromises()
    expect(w.find('[data-testid="system-warnings"]').exists()).toBe(false)
  })

  it.each([
    ['trusted_proxy_broad', 'TRUSTED_PROXY_CIDRS is too broad', 'reverse-proxy-and-trusted_proxy_cidrs'],
    ['proxy_not_trusted', 'untrusted proxy', 'reverse-proxy-and-trusted_proxy_cidrs'],
    ['webrtc_reachability', 'Voice may not work', 'nobody-can-join-voice'],
    ['webrtc_udp_mux_disabled', '51 ports', 'voice-webrtc'],
    ['voice_unavailable', 'Voice is disabled', 'nobody-can-join-voice'],
    ['turn_unavailable', 'Built-in TURN relay is not fully running', 'built-in-turn-relay'],
    ['insecure_public_url', 'PUBLIC_URL is not https', 'public_url-and-secure-cookies'],
    ['something_new', 'Server warning: something_new', 'operations.md'],
  ])('explains %s with a doc link', async (code, text, anchor) => {
    apiMock.mockResolvedValue(systemStatus({ warnings: [{ code, count: 1, since: '2026-10-05T10:00:00Z', ...(code === 'webrtc_udp_mux_disabled' ? { value: 51 } : {}) }] }))
    const w = keep(mount(AdminSystemTab))
    await flushPromises()
    const item = w.find(`[data-testid="system-warning-${code}"]`)
    expect(item.text()).toContain(text)
    const href = item.find('a').attributes('href') ?? ''
    expect(href).toContain(anchor)
    expect(href.startsWith('https://github.com/')).toBe(true)
    expect(item.find('a').attributes('rel')).toContain('noopener')
  })

  it('renders the German texts', async () => {
    setLocale('de')
    apiMock.mockResolvedValue(systemStatus({ warnings: [{ code: 'voice_unavailable', count: 1, since: '2026-10-05T10:00:00Z' }] }))
    const w = keep(mount(AdminSystemTab))
    await flushPromises()
    expect(w.find('[data-testid="system-warnings"]').text()).toContain('Sprache ist deaktiviert')
  })

  it('tolerates a udp mux warning without a value', async () => {
    apiMock.mockResolvedValue(systemStatus({ warnings: [{ code: 'webrtc_udp_mux_disabled', count: 1, since: '2026-10-05T10:00:00Z' }] }))
    const w = keep(mount(AdminSystemTab))
    await flushPromises()
    expect(w.find('[data-testid="system-warning-webrtc_udp_mux_disabled"]').text()).toContain('0 ports')
  })
})
