import type { VueWrapper } from '@vue/test-utils'
import type { ApiOptions } from '../lib/api'

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { nextTick } from 'vue'
import AdminSystemTab from './AdminSystemTab.vue'
import UpdateBanner from './UpdateBanner.vue'
import { systemStatus } from './systemStatus.fixture'
import { useAppVersionStore } from '../stores/appVersion'
import { useChatStore } from '../stores/chat'
import { setLocale } from '../i18n'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args),
  onUnauthorized: vi.fn(),
  ApiError: class ApiError extends Error {}
}))

function withUpdate(self: Partial<import('../types/domain').SelfUpdateStatus> = {}) {
  const st = systemStatus()
  st.version.current = '0.3.0'
  st.update = {
    ...st.update, current_version: '0.3.0', latest_version: '0.4.0', update_available: true,
    release_url: 'https://github.com/msch128/mnema-talk/releases/tag/v0.4.0'
  }
  st.self_update = { configured: true, image: 'ghcr.io/msch128/mnema-talk:0.4', reach: 'yes', reach_reason: 'follows', available: true, next_allowed_at: null, ...self }
  return st
}

let w: VueWrapper
beforeEach(() => {
  setLocale('en')
  setActivePinia(createPinia())
  apiMock.mockReset()
})
afterEach(() => {
  w?.unmount()
  vi.useRealTimers()
  vi.unstubAllGlobals()
  document.body.innerHTML = ''
})

describe('self-update in the System tab', () => {
  it("asks for the password, sends it with the target version and waits for the 00000000-0000-4000-8000-0000000003f1 version", async () => {
    vi.useFakeTimers()
    apiMock.mockResolvedValueOnce(withUpdate())
    w = mount(AdminSystemTab, { attachTo: document.body })
    await flushPromises()
    await w.find('[data-testid="self-update-open"]')!.trigger('click')
    await nextTick()
    const dialog = document.querySelector<HTMLElement>('[data-testid="self-update-dialog"]')!
    expect(dialog!.textContent).toContain('0.3.0 → 0.4.0')
    expect(dialog!.textContent).toContain('30–60 seconds')
    const confirm = document.querySelector<HTMLButtonElement>('[data-testid="self-update-confirm"]')!
    expect(confirm!.disabled).toBe(true)

    const input = document.querySelector<HTMLInputElement>('#self-update-password')!
    input.value = 'admin-password-123'
    input.dispatchEvent(new Event('input'))
    await nextTick()
    apiMock.mockResolvedValueOnce({ status: 'started', from_version: '0.3.0', target_version: '0.4.0' })
    dialog!.dispatchEvent(new Event('submit'))
    await flushPromises()
    expect(apiMock).toHaveBeenLastCalledWith('/api/admin/system/update', { decode: expect.any(Function),
      method: 'POST', json: { password: 'admin-password-123', target_version: '0.4.0' }
    })
    expect(w.find('[data-testid="self-update-running"]')!.exists()).toBe(true)

    // The server restarts (503), then reports the new version.
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({ ok: false, status: 503 })
      .mockResolvedValueOnce({ ok: true, json: () => Promise.resolve({ status: 'ok', version: '0.4.0' }) })
    vi.stubGlobal('fetch', fetchMock)
    await vi.advanceTimersByTimeAsync(5000)
    await vi.advanceTimersByTimeAsync(5000)
    expect(fetchMock).toHaveBeenCalledWith('/api/health', expect.anything())
    expect(useAppVersionStore().serverVersion).toBe('0.4.0')
    expect(w.find('[data-testid="self-update-running"]')!.exists()).toBe(false)
  })

  it('shows the server error and clears the password on failure', async () => {
    apiMock.mockResolvedValueOnce(withUpdate())
    w = mount(AdminSystemTab, { attachTo: document.body })
    await flushPromises()
    await w.find('[data-testid="self-update-open"]')!.trigger('click')
    await nextTick()
    const input = document.querySelector<HTMLInputElement>('#self-update-password')!
    input.value = 'wrong'
    input.dispatchEvent(new Event('input'))
    await nextTick()
    apiMock.mockRejectedValueOnce(Object.assign(new Error('The current password is incorrect.'), { code: 'FORBIDDEN' }))
    document.querySelector<HTMLElement>('[data-testid="self-update-dialog"]')!.dispatchEvent(new Event('submit'))
    await flushPromises()
    expect(document.querySelector<HTMLElement>('[data-testid="self-update-error"]')!.textContent).toContain('incorrect')
    expect(document.querySelector<HTMLInputElement>('#self-update-password')!.value).toBe('')
  })

  it('offers no button when not configured, pinned or cooling down', async () => {
    for (const [self, testid] of [
      [{ configured: false, available: false }, 'self-update-not-configured'],
      [{ available: false, reach: 'no', reach_reason: 'version_pinned', image: 'ghcr.io/msch128/mnema-talk:0.3.0' }, 'self-update-unreachable'],
      [{ available: false, next_allowed_at: '2026-10-05T12:05:00Z' }, 'self-update-cooldown']
    ] as const) {
      apiMock.mockResolvedValueOnce(withUpdate(self))
      w = mount(AdminSystemTab)
      await flushPromises()
      expect(w.find('[data-testid="self-update-open"]')!.exists()).toBe(false)
      expect(w.find(`[data-testid="${testid}"]`)!.exists()).toBe(true)
      w.unmount()
      // Current wrapper was unmounted.
    }
  })
})

describe('updating banner', () => {
  it('tells everyone about a running update until the server is back', async () => {
    const store = useAppVersionStore()
    store.setServerVersion(store.clientVersion)
    w = mount(UpdateBanner)
    useChatStore().handleWSEvent({ type: 'system_update', payload: { version: '99.1.0' } })
    await nextTick()
    expect(w.find('[data-testid="updating-banner"]')!.text()).toContain('99.1.0')
    // The restarted server reports the new version: the reload banner replaces it.
    useChatStore().handleWSEvent({ type: 'server_info', payload: { version: '99.1.0' } })
    await nextTick()
    expect(w.find('[data-testid="updating-banner"]')!.exists()).toBe(false)
    expect(w.find('[data-testid="update-banner"]')!.exists()).toBe(true)
  })
})
