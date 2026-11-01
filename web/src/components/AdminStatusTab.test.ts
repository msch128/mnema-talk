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

// The capacity panel has its own tests and its own request.
vi.mock('./AdminCapacityPanel.vue', () => ({ default: { template: '<div data-testid="capacity-stub" />' } }))

const reportMock = vi.fn<(connected: () => boolean) => Promise<void>>()
vi.mock('../lib/wsReport', () => ({ reportWebSocket: (connected: () => boolean) => reportMock(connected) }))

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
    setup: [],
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
  reportMock.mockReset()
  reportMock.mockResolvedValue(undefined)
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
    expect(w.find('[data-testid="status-warning-trusted_proxy_broad"]').text()).toContain('TRUSTED_PROXY_CIDRS is too broad')
    expect(w.find('[data-testid="status-warning-trusted_proxy_broad"]').text()).toContain('3')
  })

  it('names the port count in the UDP mux warning', async () => {
    apiMock.mockResolvedValue(statusFixture({
      warnings: [{ code: 'webrtc_udp_mux_disabled', count: 1, since: '2026-10-24T10:00:00Z', value: 101 }],
    }))
    const w = keep(mount(AdminStatusTab))
    await flushPromises()
    expect(w.get('[data-testid="status-warning-webrtc_udp_mux_disabled"]').text()).toContain('101 ports')
  })

  it('handles refresh button click and API error toast', async () => {
    apiMock.mockRejectedValueOnce(new Error('Network error'))
    apiMock.mockResolvedValue(statusFixture())
    const w = keep(mount(AdminStatusTab))
    await flushPromises()

    const toasts = useToastStore()
    expect(toasts.toasts.length).toBeGreaterThan(0)
    expect(toasts.toasts[0]?.text).toBe('Network error')

    // The list loads, the WebSocket report goes out, the list loads again; then the button.
    expect(apiMock).toHaveBeenCalledTimes(2)
    await w.find('button[aria-label="Refresh"]').trigger('click')
    await flushPromises()

    expect(apiMock).toHaveBeenCalledTimes(3)
    expect(w.find('[data-testid="version-current"]').text()).toBe('1.2.0')
  })
  it('says the version is up to date when checks are on and nothing is newer', async () => {
    apiMock.mockResolvedValue(statusFixture())
    const w = keep(mount(AdminStatusTab))
    await flushPromises()
    expect(w.find('[data-testid="status-up-to-date"]').exists()).toBe(true)
    expect(w.find('[data-testid="status-update-available"]').exists()).toBe(false)
    expect(w.find('[data-testid="status-auto-deploy"]').text()).toContain('ghcr.io/msch128/mnema-talk:1.2 (yes)')
  })

  it('says update checks are disabled and hides an empty auto-deploy image', async () => {
    const base = statusFixture()
    apiMock.mockResolvedValue(statusFixture({
      update: { ...base.update, check_enabled: false },
      auto_deploy: { ...base.auto_deploy, image: '' },
    }))
    const w = keep(mount(AdminStatusTab))
    await flushPromises()
    expect(w.find('[data-testid="status-check-disabled"]').exists()).toBe(true)
    expect(w.find('[data-testid="status-auto-deploy"]').text()).toBe('')
  })

  it('shows an unreachable database and storage with pending migrations', async () => {
    const base = statusFixture()
    apiMock.mockResolvedValue(statusFixture({
      database: { ...base.database, reachable: false, size_bytes: 0, latest_migration: '', pending_migrations: 2 },
      storage: { ...base.storage, configured: false, reachable: false, used_bytes: 500 },
    }))
    const w = keep(mount(AdminStatusTab))
    await flushPromises()
    expect(w.find('[data-testid="db-reachable"]').text()).toContain('Not reachable')
    expect(w.find('[data-testid="db-size"]').text()).toBe('0 B')
    expect(w.find('[data-testid="db-latest-migration"]').text()).toBe('–')
    expect(w.find('[data-testid="db-applied-migrations"]').text()).toContain('2')
    expect(w.find('[data-testid="storage-configured"]').text()).toBe('Not configured')
    expect(w.find('[data-testid="storage-reachable"]').text()).toContain('Not reachable')
    expect(w.find('[data-testid="storage-used-bytes"]').text()).toBe('500 B')
  })

  it('shows a dash for unknown sizes and dates, and omits the free disk when unknown', async () => {
    const base = statusFixture()
    const rest: Partial<OperatorStatus> = { ...base }
    delete rest.disk_free_bytes
    apiMock.mockResolvedValue({
      ...rest,
      version: { ...base.version, revision: '' },
      database: { ...base.database, size_bytes: null },
      warnings: [
        { code: 'no_since', count: 1, since: '' },
        { code: 'bad_since', count: 2, since: 'not-a-date' },
      ],
    })
    const w = keep(mount(AdminStatusTab))
    await flushPromises()
    expect(w.find('[data-testid="db-size"]').text()).toBe('–')
    expect(w.find('[data-testid="storage-disk-free"]').exists()).toBe(false)
    expect(w.find('[data-testid="status-warning-no_since"]').text()).toContain('–')
    expect(w.find('[data-testid="status-warning-bad_since"]').text()).toContain('–')
    expect(w.find('[data-testid="status-version"]').text()).toContain('–')
  })

  it('renders only the sections the server sent', async () => {
    const base = statusFixture()
    apiMock.mockResolvedValue({ version: base.version, warnings: [] })
    const w = keep(mount(AdminStatusTab))
    await flushPromises()
    expect(w.find('[data-testid="status-version"]').exists()).toBe(true)
    for (const id of ['activity', 'database', 'storage', 'backup']) expect(w.find(`[data-testid="status-${id}"]`).exists()).toBe(false)
    expect(w.find('[data-testid="status-up-to-date"]').exists()).toBe(false)
  })

  it('formats large sizes in the matching unit', async () => {
    const base = statusFixture()
    apiMock.mockResolvedValue(statusFixture({
      database: { ...base.database, size_bytes: 3 * 1024 ** 4 * 2000 },
      storage: { ...base.storage, used_bytes: 1536 },
    }))
    const w = keep(mount(AdminStatusTab))
    await flushPromises()
    expect(w.find('[data-testid="db-size"]').text()).toMatch(/TB$/)
    expect(w.find('[data-testid="storage-used-bytes"]').text()).toBe('1.5 KB')
  })

  it('lists the setup checks, red first, with the fix for every open item', async () => {
    apiMock.mockResolvedValue(statusFixture({
      setup: [
        { id: 'public_url', level: 'ok', detail: 'PUBLIC_URL is https://chat.example.com.' },
        { id: 'mail', level: 'warn', detail: 'No outgoing mail.', fix: 'Optional: set SMTP_HOST in .env.' },
        { id: 'voice_address', level: 'fail', detail: 'No public voice address is known.', fix: 'Set WEBRTC_NAT_1TO1_IP=<your public IPv4 address> in .env.' },
      ],
    }))
    const w = keep(mount(AdminStatusTab))
    await flushPromises()

    const rows = w.findAll('[data-testid^="setup-check-"]')
    expect(rows.map(r => r.attributes('data-level'))).toEqual(['fail', 'warn', 'ok'])
    expect(rows[0]?.text()).toContain('Voice address')
    expect(w.find('[data-testid="setup-fix-voice_address"]').text()).toContain('WEBRTC_NAT_1TO1_IP=')
    expect(w.find('[data-testid="setup-fix-mail"]').text()).toContain('SMTP_HOST')
    expect(w.find('[data-testid="setup-fix-public_url"]').exists()).toBe(false)
    expect(w.find('[data-testid="setup-summary"]').text()).toBe('2 of 3 checks need attention')
  })

  it('says so when every setup check is green, and falls back to the id for an unknown check', async () => {
    apiMock.mockResolvedValue(statusFixture({
      setup: [
        { id: 'storage', level: 'ok', detail: 'Object storage is ready.' },
        { id: 'future_check', level: 'ok', detail: 'Something newer.' },
      ],
    }))
    const w = keep(mount(AdminStatusTab))
    await flushPromises()

    expect(w.find('[data-testid="setup-summary"]').text()).toBe('Everything needed is in place')
    expect(w.find('[data-testid="setup-check-future_check"]').text()).toContain('future_check')
  })

  it('shows the check text in the display language and keeps the server sentence for an unknown code', async () => {
    apiMock.mockResolvedValue(statusFixture({
      setup: [
        { id: 'turn', level: 'warn', code: 'turn.none', params: { port: '3478' }, detail: 'No TURN relay.', fix: 'Optional: English fix.' },
        { id: 'mail', level: 'warn', code: 'mail.from_the_future', detail: 'Newer server sentence.', fix: 'Newer fix.' },
      ],
    }))
    setLocale('de')
    const w = keep(mount(AdminStatusTab))
    await flushPromises()
    expect(w.find('[data-testid="setup-check-turn"]').text()).toContain('Kein TURN-Relay')
    expect(w.find('[data-testid="setup-fix-turn"]').text()).toContain('Port 3478')
    expect(w.find('[data-testid="setup-check-mail"]').text()).toContain('Newer server sentence.')
    expect(w.find('[data-testid="setup-fix-mail"]').text()).toBe('Newer fix.')

    setLocale('en')
    await flushPromises()
    expect(w.find('[data-testid="setup-check-turn"]').text()).toContain('No TURN relay: members')
    expect(w.find('[data-testid="setup-fix-turn"]').text()).toContain('forward port 3478')
  })

  it('shows no setup section when the server sends no checks', async () => {
    apiMock.mockResolvedValue(statusFixture())
    const w = keep(mount(AdminStatusTab))
    await flushPromises()
    expect(w.find('[data-testid="status-setup"]').exists()).toBe(false)
  })

  it('reports its own WebSocket first, then reloads the list so the WebSocket item reflects it', async () => {
    const order: string[] = []
    reportMock.mockImplementation(async () => { order.push('report') })
    apiMock.mockImplementation(async () => { order.push('status'); return statusFixture() })
    keep(mount(AdminStatusTab))
    await flushPromises()
    expect(order).toEqual(['status', 'report', 'status'])
  })

  it('shows a neutral item below the amber ones, not counted as needing attention', async () => {
    apiMock.mockResolvedValue(statusFixture({
      setup: [
        { id: 'storage', level: 'ok', detail: 'Object storage is ready.' },
        { id: 'voice_udp', level: 'info', code: 'voice_udp.unconfirmed', params: { port: '50000', port_min: '50000', port_max: '50050' }, detail: 'Listening – not yet confirmed from outside.', fix: 'Forward UDP.' },
        { id: 'mail', level: 'warn', detail: 'No outgoing mail.', fix: 'Optional: set SMTP_HOST in .env.' },
      ],
    }))
    const w = keep(mount(AdminStatusTab))
    await flushPromises()
    expect(w.findAll('[data-testid^="setup-check-"]').map(r => r.attributes('data-level'))).toEqual(['warn', 'info', 'ok'])
    expect(w.find('[data-testid="setup-summary"]').text()).toBe('1 of 3 checks need attention')
    const voice = w.find('[data-testid="setup-check-voice_udp"]').text()
    expect(voice).toContain('Listening – not yet confirmed from outside')
    expect(voice).toContain('Not verified')
  })

  it('shows the confirmation with the moment in the viewer language, and the WebSocket item', async () => {
    apiMock.mockResolvedValue(statusFixture({
      setup: [
        { id: 'voice_udp', level: 'ok', code: 'voice_udp.confirmed', params: { port: '50000', at: '2026-10-29T21:30:00Z', time: '2026-10-29 21:30 UTC' }, detail: 'Confirmed.' },
        { id: 'websocket', level: 'warn', code: 'websocket.failing', params: { count: '4', at: '', time: '2026-10-29 21:30 UTC' }, detail: 'x', fix: 'y' },
      ],
    }))
    const w = keep(mount(AdminStatusTab))
    await flushPromises()
    const voice = w.find('[data-testid="setup-check-voice_udp"]').text()
    expect(voice).toContain('Confirmed: a member from outside connected (')
    expect(voice).not.toContain('UTC')
    const ws = w.find('[data-testid="setup-check-websocket"]')
    expect(ws.text()).toContain('WebSocket')
    expect(w.find('[data-testid="setup-fix-websocket"]').text()).toContain('Upgrade: websocket')
  })

  it('offers Test my connection', async () => {
    apiMock.mockResolvedValue(statusFixture())
    const w = keep(mount(AdminStatusTab))
    await flushPromises()
    expect(w.find('[data-testid="status-connection-test"] [data-testid="connection-test-run"]').exists()).toBe(true)
  })
})
