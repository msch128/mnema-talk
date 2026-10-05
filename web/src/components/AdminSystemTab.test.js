import { describe, it, expect, beforeEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import AdminSystemTab from './AdminSystemTab.vue'
import { setLocale } from '../i18n'

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

  it('shows an error toast when loading fails', async () => {
    apiMock.mockRejectedValue(new Error('boom'))
    const w = mount(AdminSystemTab)
    await flushPromises()
    expect(w.find('[data-testid="system-version"]').exists()).toBe(false)
  })
})
