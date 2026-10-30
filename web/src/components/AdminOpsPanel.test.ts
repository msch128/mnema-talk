import type { ApiOptions } from '../lib/api'

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises, enableAutoUnmount } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import AdminOpsPanel from './AdminOpsPanel.vue'
import Sparkline from './Sparkline.vue'
import { setLocale } from '../i18n'
import { opsStatus, opsSample } from './opsStatus.fixture'

// A panel left mounted would keep answering visibility changes in the next test.
enableAutoUnmount(afterEach)

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../lib/api')>()),
  api: (...args: [path: string, options?: ApiOptions]) => apiMock(...args),
  onUnauthorized: vi.fn(),
  ApiError: class ApiError extends Error {}
}))

beforeEach(() => {
  setActivePinia(createPinia())
  setLocale('en')
  apiMock.mockReset()
})
afterEach(() => {
  vi.useRealTimers()
})

function tile(w: ReturnType<typeof mount>, key: string) {
  return w.find(`[data-testid="ops-tile-${key}"]`)
}

describe('AdminOpsPanel', () => {
  it('shows the latest values with a chart per tile and totals since start', async () => {
    apiMock.mockResolvedValue(opsStatus())
    const w = mount(AdminOpsPanel)
    await flushPromises()
    expect(apiMock).toHaveBeenCalledWith('/api/admin/system/ops', { decode: expect.any(Function) })

    expect(tile(w, 'cpu').find('[data-testid="ops-value"]').text()).toBe('7 %')
    expect(tile(w, 'memory').find('[data-testid="ops-value"]').text()).toBe('102 MB')
    expect(tile(w, 'egress').find('[data-testid="ops-value"]').text()).toBe('2.5 Mbit/s')
    expect(tile(w, 'ingress').find('[data-testid="ops-value"]').text()).toBe('800 bit/s')
    expect(tile(w, 'online').find('[data-testid="ops-value"]').text()).toBe('3')
    expect(w.findAllComponents(Sparkline)).toHaveLength(12)
    expect(tile(w, 'cpu').find('svg').attributes('aria-label')).toBe('CPU (one core = 100 %), last hour: now 7 %, peak 7 %')
    expect(w.find('[data-testid="ops-collecting"]').exists()).toBe(false)

    const totals = w.find('[data-testid="ops-totals"]').text()
    expect(totals).toContain('Requests1,234')
    expect(totals).toContain('Voice connections failed1')
    expect(totals).toContain('Media forwarded (out / in)5 MB / 1 KB')
    expect(totals).toContain('GC cycles (total pause)30 (12 ms)')
  })

  it('highlights errors and failed voice connections only when they are non-zero', async () => {
    apiMock.mockResolvedValue(opsStatus())
    const w = mount(AdminOpsPanel)
    await flushPromises()
    expect(tile(w, 'errors').find('[data-testid="ops-value"]').classes()).toContain('text-mnema-warning')
    expect(tile(w, 'iceFailures').find('[data-testid="ops-value"]').classes()).not.toContain('text-mnema-warning')
  })

  it('falls back to the Go heap where the platform reports no resident memory', async () => {
    apiMock.mockResolvedValue(opsStatus({ history: [opsSample(0, { rss_bytes: 0 }), opsSample(1, { rss_bytes: 0 })] }))
    const w = mount(AdminOpsPanel)
    await flushPromises()
    expect(tile(w, 'memory').find('[data-testid="ops-value"]').text()).toBe('40 MB')
  })

  it('says it is collecting while the history is still short, and survives an empty one', async () => {
    apiMock.mockResolvedValue(opsStatus({ history: [] }))
    const w = mount(AdminOpsPanel)
    await flushPromises()
    expect(w.find('[data-testid="ops-collecting"]').exists()).toBe(true)
    expect(tile(w, 'cpu').find('[data-testid="ops-value"]').text()).toBe('0 %')
    expect(tile(w, 'egress').find('[data-testid="ops-value"]').text()).toBe('0 bit/s')
  })

  it('formats large rates and sizes with the right unit', async () => {
    apiMock.mockResolvedValue(opsStatus({
      history: [opsSample(0, { egress_bps: 3e9, ingress_bps: 4e3, rss_bytes: 3 * 1024 ** 3 }), opsSample(1, { egress_bps: 3e9, ingress_bps: 4e3, rss_bytes: 3 * 1024 ** 3 })]
    }))
    const w = mount(AdminOpsPanel)
    await flushPromises()
    expect(tile(w, 'egress').find('[data-testid="ops-value"]').text()).toBe('3 Gbit/s')
    expect(tile(w, 'ingress').find('[data-testid="ops-value"]').text()).toBe('4 kbit/s')
    expect(tile(w, 'memory').find('[data-testid="ops-value"]').text()).toBe('3 GB')
  })

  it('says live numbers are unavailable when the first request fails, and keeps old data when a later one fails', async () => {
    apiMock.mockRejectedValueOnce(new Error('offline'))
    const w = mount(AdminOpsPanel)
    await flushPromises()
    expect(w.find('[data-testid="ops-unavailable"]').exists()).toBe(true)
    expect(w.find('[data-testid="ops-totals"]').exists()).toBe(false)

    apiMock.mockResolvedValueOnce(opsStatus())
    await (w.vm as unknown as { refresh: () => Promise<void> }).refresh()
    await flushPromises()
    expect(w.find('[data-testid="ops-totals"]').exists()).toBe(true)

    apiMock.mockRejectedValueOnce(new Error('offline'))
    await (w.vm as unknown as { refresh: () => Promise<void> }).refresh()
    await flushPromises()
    expect(w.find('[data-testid="ops-totals"]').exists()).toBe(true)
    expect(w.find('[data-testid="ops-unavailable"]').exists()).toBe(false)
  })

  it('refreshes every 10 seconds while visible, skips hidden tabs and stops on unmount', async () => {
    vi.useFakeTimers()
    apiMock.mockResolvedValue(opsStatus())
    const w = mount(AdminOpsPanel)
    await flushPromises()
    expect(apiMock).toHaveBeenCalledTimes(1)

    await vi.advanceTimersByTimeAsync(10_000)
    expect(apiMock).toHaveBeenCalledTimes(2)

    const visibility = vi.spyOn(document, 'visibilityState', 'get')
    visibility.mockReturnValue('hidden')
    document.dispatchEvent(new Event('visibilitychange'))
    await vi.advanceTimersByTimeAsync(60_000)
    expect(apiMock).toHaveBeenCalledTimes(2)
    // Coming back looks at once instead of waiting for the next tick.
    visibility.mockReturnValue('visible')
    document.dispatchEvent(new Event('visibilitychange'))
    await flushPromises()
    expect(apiMock).toHaveBeenCalledTimes(3)
    visibility.mockRestore()

    w.unmount()
    await vi.advanceTimersByTimeAsync(30_000)
    expect(apiMock).toHaveBeenCalledTimes(3)
  })
})

describe('Sparkline', () => {
  it('draws nothing but the baseline for fewer than two points', () => {
    const w = mount(Sparkline, { props: { values: [3], label: 'x' } })
    expect(w.find('polyline').exists()).toBe(false)
    expect(w.find('line').exists()).toBe(true)
  })

  it('scales a series into the box and keeps a flat series on the baseline', () => {
    const rising = mount(Sparkline, { props: { values: [0, 5, 10], label: 'x' } })
    const pts = rising.find('polyline').attributes('points')!.split(' ')
    expect(pts).toHaveLength(3)
    expect(pts[0]!.split(',')[1]).toBe('26.0')
    expect(pts[2]!.split(',')[1]).toBe('2.0')

    const flat = mount(Sparkline, { props: { values: [4, 4, 4], label: 'x' } })
    expect(flat.find('polyline').attributes('points')!.split(' ')[0]!.split(',')[1]).toBe('2.0')

    const zero = mount(Sparkline, { props: { values: [0, 0], label: 'x' } })
    expect(zero.find('polyline').attributes('points')!.split(' ')[0]!.split(',')[1]).toBe('26.0')
  })
})
