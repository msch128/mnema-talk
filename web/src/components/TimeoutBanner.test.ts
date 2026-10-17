import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import TimeoutBanner from './TimeoutBanner.vue'
import { setLocale } from '../i18n'
import { usePermissionsStore } from '../stores/permissions'

vi.mock('../lib/api', async (importOriginal) => ({ ...(await importOriginal<typeof import('../lib/api')>()), api: vi.fn() }))

function info(until?: string, reason?: string) {
  return { owner: false, permissions: 0, role_ids: [], top_position: 0, max_upload_mb: 50, catalog: [], channels: [], ...(until ? { timeout_until: until } : {}), ...(reason ? { timeout_reason: reason } : {}) }
}

beforeEach(() => {
  vi.useFakeTimers()
  vi.setSystemTime(new Date('2026-01-01T12:00:00Z'))
  setActivePinia(createPinia())
  setLocale('en')
})
afterEach(() => vi.useRealTimers())

describe('TimeoutBanner', () => {
  it('stays away without a timeout, and for one that is over', async () => {
    const permissions = usePermissionsStore()
    const w = mount(TimeoutBanner)
    expect(w.find('[data-testid="timeout-banner"]').exists()).toBe(false)
    permissions.info = info('2026-01-01T11:00:00Z')
    await vi.advanceTimersByTimeAsync(0)
    expect(w.find('[data-testid="timeout-banner"]').exists()).toBe(false)
  })

  it('counts down with the reason and asks for the permissions again when it ends', async () => {
    const permissions = usePermissionsStore()
    const load = vi.spyOn(permissions, 'load').mockResolvedValue()
    const w = mount(TimeoutBanner)
    permissions.info = info('2026-01-01T12:00:03Z', 'calm down')
    await vi.advanceTimersByTimeAsync(0)
    expect(w.get('[data-testid="timeout-banner"]').text()).toContain('You are timed out.')
    expect(w.get('[data-testid="timeout-banner"]').text()).toContain('calm down')
    expect(w.get('[data-testid="timeout-remaining"]').text()).toBe('0:03')
    await vi.advanceTimersByTimeAsync(1000)
    expect(w.get('[data-testid="timeout-remaining"]').text()).toBe('0:02')
    expect(load).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(2500)
    expect(w.find('[data-testid="timeout-banner"]').exists()).toBe(false)
    expect(load).toHaveBeenCalledTimes(1)
    // No ticking goes on afterwards.
    await vi.advanceTimersByTimeAsync(5000)
    expect(load).toHaveBeenCalledTimes(1)
  })

  it('follows a changed or removed timeout and stops its timer on unmount', async () => {
    const permissions = usePermissionsStore()
    const w = mount(TimeoutBanner)
    permissions.info = info('2026-01-01T13:00:00Z')
    await vi.advanceTimersByTimeAsync(0)
    expect(w.get('[data-testid="timeout-remaining"]').text()).toBe('1:00:00')
    expect(w.text()).not.toContain('calm')
    permissions.info = info()
    await vi.advanceTimersByTimeAsync(0)
    expect(w.find('[data-testid="timeout-banner"]').exists()).toBe(false)
    permissions.info = info('2026-01-01T13:00:00Z')
    await vi.advanceTimersByTimeAsync(0)
    w.unmount()
    expect(vi.getTimerCount()).toBe(0)
  })
})
