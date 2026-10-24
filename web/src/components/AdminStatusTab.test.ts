import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import type { ApiOptions } from '../lib/api'
import AdminStatusTab from './AdminStatusTab.vue'
import { useToastStore } from '../stores/toast'
import { setLocale } from '../i18n'
import type { OperatorStatus } from '../types/domain'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async (original) => ({
  ...(await original<typeof import('../lib/api')>()),
  api: (path: string, options?: ApiOptions) => apiMock(path, options),
}))

function statusFixture(overrides: Partial<OperatorStatus> = {}): OperatorStatus {
  return {
    version: {
      current: '1.2.0',
      revision: 'abc1234',
      go_version: 'go1.23.0',
    },
    update: {
      check_enabled: true,
      current_version: '1.2.0',
      latest_version: '1.2.0',
      update_available: false,
      release_url: 'https://github.com/msch128/mnema-talk/releases/tag/v1.2.0',
      release_notes: '',
      published_at: null,
      checked_at: '2026-10-24T12:00:00Z',
      check_error: '',
      retry_at: null,
    },
    auto_deploy: {
      configured: false,
      image: 'ghcr.io/msch128/mnema-talk:1.2',
      reach: 'yes',
      reach_reason: 'follows',
      available: false,
      next_allowed_at: null,
    },
    database: {
      reachable: true,
      size_bytes: 12582912,
      latest_migration: '0071_example.sql',
      applied_migrations: 71,
      pending_migrations: 0,
    },
    storage: {
      configured: true,
      reachable: true,
      used_bytes: 4194304,
    },
    disk_free_bytes: 10737418240,
    backup: {
      last_backup_age: 'unknown',
      last_restore_test_age: 'unknown',
      hint: 'doc/operations.md#backups-and-restore',
    },
    activity: {
      active_sessions: 3,
      connected_websocket_clients: 5,
      active_talks: 1,
      voice_participants: 2,
    },
    warnings: [],
    ...overrides,
  }
}

let wrappers: VueWrapper[] = []
function keep<T extends VueWrapper>(w: T): T {
  wrappers.push(w)
  return w
}

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
})

afterEach(() => {
  for (const w of wrappers) w.unmount()
  wrappers = []
  vi.restoreAllMocks()
  document.body.innerHTML = ''
})

describe('AdminStatusTab', () => {
  it('loads and renders operator status details', async () => {
    apiMock.mockResolvedValue(statusFixture())
    const w = keep(mount(AdminStatusTab))
    await flushPromises()

    expect(apiMock).toHaveBeenCalledWith('/api/admin/status', expect.objectContaining({ decode: expect.any(Function) }))

    expect(w.find('[data-testid="version-current"]').text()).toBe('1.2.0')
    expect(w.find('[data-testid="db-reachable"]').text()).toContain('Reachable')
    expect(w.find('[data-testid="db-size"]').text()).toBe('12 MB')
    expect(w.find('[data-testid="db-latest-migration"]').text()).toBe('0071_example.sql')
    expect(w.find('[data-testid="db-applied-migrations"]').text()).toContain('71')

    expect(w.find('[data-testid="storage-configured"]').text()).toBe('Configured')
    expect(w.find('[data-testid="storage-reachable"]').text()).toContain('Reachable')
    expect(w.find('[data-testid="storage-used-bytes"]').text()).toBe('4 MB')
    expect(w.find('[data-testid="storage-disk-free"]').text()).toBe('10 GB')

    expect(w.find('[data-testid="backup-last-age"]').text()).toBe('unknown')
    expect(w.find('[data-testid="backup-last-restore"]').text()).toBe('unknown')
    expect(w.find('[data-testid="backup-hint"]').text()).toContain('operations guide')

    expect(w.find('[data-testid="activity-active-sessions"]').text()).toBe('3')
    expect(w.find('[data-testid="activity-connected-ws"]').text()).toBe('5')
    expect(w.find('[data-testid="activity-active-talks"]').text()).toBe('1')
    expect(w.find('[data-testid="activity-voice-participants"]').text()).toBe('2')
    expect(w.find('[data-testid="status-warnings"]').exists()).toBe(false)
  })

  it('renders available updates and operator warnings', async () => {
    apiMock.mockResolvedValue(statusFixture({
      update: {
        check_enabled: true,
        current_version: '1.2.0',
        latest_version: '1.3.0',
        update_available: true,
        release_url: 'https://github.com/msch128/mnema-talk/releases/tag/v1.3.0',
        release_notes: '',
        published_at: null,
        checked_at: '2026-10-24T12:00:00Z',
        check_error: '',
        retry_at: null,
      },
      warnings: [
        { code: 'trusted_proxy_broad', count: 3, since: '2026-10-24T10:00:00Z' },
      ],
    }))

    const w = keep(mount(AdminStatusTab))
    await flushPromises()

    expect(w.find('[data-testid="status-update-available"]').text()).toContain('1.3.0')
    expect(w.find('[data-testid="status-warnings"]').exists()).toBe(true)
    expect(w.find('[data-testid="status-warning-trusted_proxy_broad"]').text()).toContain('trusted_proxy_broad')
    expect(w.find('[data-testid="status-warning-trusted_proxy_broad"]').text()).toContain('3')
  })

  it('handles refresh button click and API error toast', async () => {
    apiMock.mockRejectedValueOnce(new Error('Network error'))
    const w = keep(mount(AdminStatusTab))
    await flushPromises()

    const toasts = useToastStore()
    expect(toasts.toasts.length).toBeGreaterThan(0)
    expect(toasts.toasts[0]?.text).toBe('Network error')

    apiMock.mockResolvedValueOnce(statusFixture())
    await w.find('button[aria-label="Refresh"]').trigger('click')
    await flushPromises()

    expect(apiMock).toHaveBeenCalledTimes(2)
    expect(w.find('[data-testid="version-current"]').text()).toBe('1.2.0')
  })
})
