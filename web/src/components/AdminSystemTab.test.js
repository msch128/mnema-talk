import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import AdminSystemTab from './AdminSystemTab.vue'
import { setLocale } from '../i18n'
import { useAppVersionStore } from '../stores/appVersion'

export function systemStatus(overrides = {}) {
  return {
    version: { current: '0.4.0', revision: 'abc123def456', go_version: 'go1.27.0' },
    health: {
      database: { reachable: true, latest_migration: '0012_message_count.sql', applied_migrations: 12, pending_migrations: 0 },
      storage: { configured: true, reachable: true, files: 3, total_bytes: 3 * 1024 * 1024, attachment_bytes: 2 * 1024 * 1024, avatar_bytes: 1024 * 1024 },
      voice: {
        enabled: true, rooms: 1, participants: 3, media_connections: 3, screen_shares: 1, cameras: 2,
        websocket_connections: 5, online_users: 4, turn_configured: true, stun_configured: false
      },
      runtime: { started_at: '2026-10-05T10:00:00Z', uptime_seconds: 93784, go_version: 'go1.27.0', goroutines: 42, mem_alloc_bytes: 10 * 1024 * 1024, mem_sys_bytes: 20 * 1024 * 1024 }
    },
    update: {
      check_enabled: true, current_version: '0.4.0', latest_version: '0.4.0', update_available: false,
      release_url: '', release_notes: '', published_at: null, checked_at: '2026-10-05T11:00:00Z', check_error: '', retry_at: null
    },
    ...overrides
  }
}

const apiMock = vi.fn()
vi.mock('../lib/api', () => ({
  api: (...args) => apiMock(...args),
  onUnauthorized: vi.fn(),
  ApiError: class ApiError extends Error {}
}))

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
})

describe('AdminSystemTab', () => {
  it('shows version and health from /api/admin/system', async () => {
    apiMock.mockResolvedValue(systemStatus())
    const w = mount(AdminSystemTab)
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith('/api/admin/system')
    const version = w.find('[data-testid="system-version"]').text()
    expect(version).toContain('0.4.0')
    expect(version).toContain('abc123def456')
    const health = w.find('[data-testid="system-health"]').text()
    expect(health).toContain('0012_message_count.sql')
    expect(health).toContain('Reachable')
    expect(w.find('[data-testid="system-storage-size"]').text()).toContain('3 MB in 3 files')
    expect(w.find('[data-testid="system-uptime"]').text()).toBe('1 d 2 h')
    expect(health).toContain('TURN configured')
  })

  it('reports unreachable dependencies', async () => {
    const st = systemStatus()
    st.health.database.reachable = false
    st.health.storage.reachable = false
    apiMock.mockResolvedValue(st)
    const w = mount(AdminSystemTab)
    await flushPromises()
    expect(w.find('[data-testid="system-health"]').text()).toContain('Not reachable')
  })

  it('shows an available update with plain-text notes and a release link', async () => {
    const st = systemStatus()
    st.update = {
      ...st.update, current_version: '0.3.0', latest_version: '0.4.0', update_available: true,
      release_url: 'https://github.com/msch128/mnema-talk/releases/tag/v0.4.0',
      release_notes: '<img src=x onerror=alert(1)> **bold**'
    }
    apiMock.mockResolvedValue(st)
    const w = mount(AdminSystemTab)
    await flushPromises()
    expect(w.find('[data-testid="update-available"]').text()).toContain('0.3.0 → 0.4.0')
    const notes = w.find('[data-testid="update-notes"]')
    expect(notes.text()).toBe('<img src=x onerror=alert(1)> **bold**')
    expect(notes.find('img').exists()).toBe(false)
    const link = w.find('[data-testid="update-release-link"]')
    expect(link.attributes('href')).toBe('https://github.com/msch128/mnema-talk/releases/tag/v0.4.0')
    expect(link.attributes('rel')).toContain('noopener')
    expect(w.find('[data-testid="update-command"]').text()).toContain('docker compose pull app')
    expect(useAppVersionStore().adminUpdateAvailable).toBe(true)
  })

  it('checks now on request', async () => {
    apiMock.mockResolvedValueOnce(systemStatus())
    const w = mount(AdminSystemTab)
    await flushPromises()
    apiMock.mockResolvedValueOnce({ ...systemStatus().update, latest_version: '0.5.0', update_available: true, current_version: '0.4.0' })
    await w.find('[data-testid="update-check-now"]').trigger('click')
    await flushPromises()
    expect(apiMock).toHaveBeenLastCalledWith('/api/admin/system/check', { method: 'POST' })
    expect(w.find('[data-testid="update-available"]').text()).toContain('0.5.0')
  })

  it('says when the update check is disabled', async () => {
    const st = systemStatus()
    st.update = { ...st.update, check_enabled: false, latest_version: '' }
    apiMock.mockResolvedValue(st)
    const w = mount(AdminSystemTab)
    await flushPromises()
    expect(w.find('[data-testid="update-check-disabled"]').exists()).toBe(true)
    expect(w.find('[data-testid="update-check-now"]').exists()).toBe(false)
  })

  it('shows an error toast when loading fails', async () => {
    apiMock.mockRejectedValue(new Error('boom'))
    const w = mount(AdminSystemTab)
    await flushPromises()
    expect(w.find('[data-testid="system-version"]').exists()).toBe(false)
  })
})
