import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import UserBar from './UserBar.vue'
import { useAuthStore } from '../stores/auth'
import { useChatStore } from '../stores/chat'
import { useAppVersionStore, ADMIN_UPDATE_POLL_MS } from '../stores/appVersion'
import { setLocale } from '../i18n'

vi.mock('../composables/useWebRTC', () => ({
  useWebRTC: () => ({ leaveVoiceChannel: vi.fn(), startScreenShare: vi.fn(), stopScreenShare: vi.fn() })
}))

const available = { check_enabled: true, current_version: '0.3.0', latest_version: '0.4.0', update_available: true }

let w
beforeEach(() => {
  setLocale('en')
  setActivePinia(createPinia())
  vi.stubGlobal('fetch', vi.fn(() => Promise.resolve({ status: 200, ok: true, json: () => Promise.resolve(available) })))
})
afterEach(() => {
  w?.unmount()
  vi.unstubAllGlobals()
  vi.useRealTimers()
  document.body.innerHTML = ''
})

describe('admin update badge', () => {
  it('shows a dot on the menu and a badge on the admin console entry', async () => {
    const auth = useAuthStore()
    auth.user = { id: 'u1', display_name: 'Herzog', role: 'admin' }
    useAppVersionStore().setAdminUpdate(available)
    w = mount(UserBar, { attachTo: document.body })
    expect(w.find('[data-testid="admin-update-dot"]').exists()).toBe(true)
    await w.find('[data-testid="account-menu-button"]').trigger('click')
    expect(w.find('[data-testid="admin-update-badge"]').text()).toBe('Update 0.4.0')
  })

  it('is never shown to members, even with stale data', async () => {
    const auth = useAuthStore()
    auth.user = { id: 'u2', display_name: 'Max', role: 'member' }
    useAppVersionStore().setAdminUpdate(available)
    w = mount(UserBar, { attachTo: document.body })
    expect(w.find('[data-testid="admin-update-dot"]').exists()).toBe(false)
    await w.find('[data-testid="account-menu-button"]').trigger('click')
    expect(w.find('[data-testid="admin-update-badge"]').exists()).toBe(false)
  })

  it('admins fetch the update status on connect and every 30 minutes; members never', async () => {
    vi.useFakeTimers()
    const auth = useAuthStore()
    auth.user = { id: 'u1', display_name: 'Herzog', role: 'admin' }
    useChatStore().handleWSEvent({ type: 'server_info', payload: { version: '0.3.0' } })
    await flushPromises()
    expect(fetch).toHaveBeenCalledTimes(1)
    expect(fetch.mock.calls[0][0]).toBe('/api/admin/system/update')
    expect(useAppVersionStore().adminUpdateAvailable).toBe(true)
    vi.advanceTimersByTime(ADMIN_UPDATE_POLL_MS)
    await flushPromises()
    expect(fetch).toHaveBeenCalledTimes(2)

    auth.user = { id: 'u1', display_name: 'Herzog', role: 'member' }
    vi.advanceTimersByTime(ADMIN_UPDATE_POLL_MS)
    await flushPromises()
    expect(fetch).toHaveBeenCalledTimes(2)
    expect(useAppVersionStore().adminUpdateAvailable).toBe(false)
  })
})
