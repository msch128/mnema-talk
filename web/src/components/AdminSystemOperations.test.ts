import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { nextTick } from 'vue'
import type { ApiOptions } from '../lib/api'
import { ApiError } from '../lib/api'
import AdminSystemTab from './AdminSystemTab.vue'
import SelfUpdateDialog from './SelfUpdateDialog.vue'
import { systemStatus } from './systemStatus.fixture'
import { useToastStore } from '../stores/toast'
import { userFixture } from '../test-fixtures.fixture'
import { errors, button } from './adminTestkit.fixture'
import { useAuthStore } from '../stores/auth'
import { setLocale } from '../i18n'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async (original) => ({
  ...(await original<typeof import('../lib/api')>()),
  api: (path: string, options?: ApiOptions) => apiMock(path, options),
}))
// The live operations panel has its own tests and endpoint.
vi.mock('./AdminOpsPanel.vue', () => ({ default: { template: '<div />' } }))
vi.mock('./AdminUploadLimit.vue', () => ({ default: { template: '<div />' } }))

let wrappers: VueWrapper[] = []
function keep<T extends VueWrapper>(w: T): T {
  wrappers.push(w)
  return w
}
beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
  useAuthStore().user = userFixture({ role: 'admin', username: 'owner' }) // the Owner may do everything
})
afterEach(() => {
  for (const w of wrappers) w.unmount()
  wrappers = []
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  vi.useRealTimers()
  document.body.innerHTML = ''
})
describe('admin system operational failures', () => {
  async function openSystem(st = systemStatus()) {
    apiMock.mockResolvedValueOnce(st)
    const w = keep(mount(AdminSystemTab))
    await flushPromises()
    return w
  }
  it.each([
    new ApiError(429, 'RATE_LIMITED', 'limited'),
    new Error('check failed'),
    null,
  ])(
    'reports failed immediate checks and re-enables the action',
    async (reason) => {
      const w = await openSystem()
      apiMock.mockRejectedValueOnce(reason)
      await w.find('[data-testid="update-check-now"]').trigger('click')
      await flushPromises()
      expect(errors()).toHaveLength(1)
      expect(
        w.find('[data-testid="update-check-now"]').attributes('disabled'),
      ).toBeUndefined()
    },
  )
  it('reports release service errors then up-to-date status', async () => {
    const w = await openSystem()
    apiMock.mockResolvedValueOnce({
      ...systemStatus().update,
      check_error: 'upstream offline',
    })
    await w.find('[data-testid="update-check-now"]').trigger('click')
    await flushPromises()
    expect(errors()[0]).toContain('upstream offline')
    apiMock.mockResolvedValueOnce(systemStatus().update)
    await w.find('[data-testid="update-check-now"]').trigger('click')
    await flushPromises()
    expect(useToastStore().toasts.some((t) => t.type === 'success')).toBe(true)
  })
  it('copies a manual update command, reports clipboard denial, and closes confirmation', async () => {
    const st = systemStatus()
    st.update.update_available = true
    st.self_update = { ...st.self_update, configured: true, available: true }
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('navigator', { clipboard: { writeText } })
    const w = await openSystem(st)
    await button(w, 'Copy').trigger('click')
    await flushPromises()
    expect(writeText).toHaveBeenCalledWith(
      'docker compose pull app && docker compose up -d app',
    )
    writeText.mockRejectedValueOnce(new Error('denied'))
    await button(w, 'Copy').trigger('click')
    await flushPromises()
    expect(errors()).toHaveLength(1)
    await w.find('[data-testid="self-update-open"]').trigger('click')
    w.findComponent(SelfUpdateDialog).vm.$emit('close')
    await nextTick()
    expect(w.findComponent(SelfUpdateDialog).exists()).toBe(false)
  })
  it.each([0, 3700])(
    'formats short uptime, missing metadata, and absent configuration (%s seconds)',
    async (seconds) => {
      const st = systemStatus()
      st.version.revision = ''
      st.update.checked_at = 'invalid date'
      st.update.latest_version = ''
      st.health.database.latest_migration = ''
      st.health.database.pending_migrations = 2
      st.health.storage.configured = false
      st.health.storage.total_bytes = 0
      st.health.runtime.uptime_seconds = seconds
      st.health.voice.enabled = false
      st.health.voice.turn_configured = false
      const w = await openSystem(st)
      expect(w.find('[data-testid="system-health"]').text()).toContain('0 B')
      expect(w.find('[data-testid="system-uptime"]').text()).toContain(
        seconds ? '1 h' : '0 min',
      )
      expect(w.text()).toContain('Not configured')
    },
  )
  it.each([
    ['running and reachable', { active: true, reachability: 'reachable' as const }, 'Port 3479 (UDP+TCP), 2 relayed, reachable from here'],
    ['running, self-test failed', { active: true, reachability: 'unreachable' as const }, 'not reachable from here, check the port forward'],
    ['asked for but not running', { active: false, enabled: true }, 'Not running'],
    ['switched off', { active: false, enabled: false }, 'Off'],
  ])('shows the built-in TURN relay: %s', async (_name, change, text) => {
    const st = systemStatus()
    Object.assign(st.health.voice.builtin_turn, change)
    const w = await openSystem(st)
    expect(w.find('[data-testid="system-builtin-turn"]').text()).toContain(text)
  })
  it('shows a placeholder for a missing check time and guards a check while status is absent', async () => {
    const w = await openSystem()
    apiMock.mockResolvedValueOnce({
      ...systemStatus().update,
      checked_at: null,
    })
    await w.find('[data-testid="update-check-now"]').trigger('click')
    await flushPromises()
    expect(w.text()).toContain('–')
  })
  it('polls old version, malformed health, and network failure until stalled, then stops on unmount', async () => {
    vi.useFakeTimers()
    const st = systemStatus()
    st.update.update_available = true
    st.self_update = { ...st.self_update, configured: true, available: true }
    const w = await openSystem(st)
    await w.find('[data-testid="self-update-open"]').trigger('click')
    const fetchMock = vi.fn().mockRejectedValue(new Error('restart'))
    vi.stubGlobal('fetch', fetchMock)
    w.findComponent(SelfUpdateDialog).vm.$emit('started', {
      status: 'started',
      from_version: '',
      target_version: '0.5.0',
    })
    await nextTick()
    fetchMock
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ status: 'ok', version: '0.4.0' }),
      })
      .mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve({ status: 'ok', version: '' }),
      })
    await vi.advanceTimersByTimeAsync(310000)
    expect(w.find('[data-testid="self-update-stalled"]').exists()).toBe(true)
    const count = fetchMock.mock.calls.length
    await vi.advanceTimersByTimeAsync(15000)
    expect(fetchMock).toHaveBeenCalledTimes(count)
  })
  it('clears an active health poll when the component unmounts', async () => {
    vi.useFakeTimers()
    const st = systemStatus()
    st.update.update_available = true
    st.self_update = { ...st.self_update, configured: true, available: true }
    const w = await openSystem(st)
    await w.find('[data-testid="self-update-open"]').trigger('click')
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)
    w.findComponent(SelfUpdateDialog).vm.$emit('started', {
      status: 'started',
      from_version: '0.4.0',
      target_version: '0.5.0',
    })
    await nextTick()
    w.unmount()
    wrappers = wrappers.filter((x) => x !== w)
    await vi.advanceTimersByTimeAsync(10000)
    expect(fetchMock).not.toHaveBeenCalled()
  })
  it('does not re-arm the poll when the tab is left while a health request is in flight', async () => {
    vi.useFakeTimers()
    const st = systemStatus()
    st.update.update_available = true
    st.self_update = { ...st.self_update, configured: true, available: true }
    const w = await openSystem(st)
    await w.find('[data-testid="self-update-open"]').trigger('click')
    let answer!: (r: unknown) => void
    const fetchMock = vi.fn().mockImplementation(() => new Promise((resolve) => { answer = resolve }))
    vi.stubGlobal('fetch', fetchMock)
    w.findComponent(SelfUpdateDialog).vm.$emit('started', { status: 'started', from_version: '0.4.0', target_version: '0.5.0' })
    await nextTick()
    await vi.advanceTimersByTimeAsync(5000)
    expect(fetchMock).toHaveBeenCalledTimes(1)
    w.unmount()
    wrappers = wrappers.filter((x) => x !== w)
    answer({ ok: false })
    await vi.advanceTimersByTimeAsync(20000)
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })
})
