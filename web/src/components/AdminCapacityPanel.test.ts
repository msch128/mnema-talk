import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import type { ApiOptions } from '../lib/api'
import AdminCapacityPanel from './AdminCapacityPanel.vue'
import { setLocale } from '../i18n'
import type { ServerCapacityStatus } from '../types/rest'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async original => ({
  ...(await original<typeof import('../lib/api')>()),
  api: (path: string, options?: ApiOptions) => apiMock(path, options),
}))

function fixture(over: Partial<ServerCapacityStatus> = {}, estimate: Partial<ServerCapacityStatus['estimate']> = {}): ServerCapacityStatus {
  return {
    collecting: false, cpu_cores: 4, cpu_used_cores: 0.46, memory_limit_bytes: 1 << 30, rss_bytes: 300 << 20, heap_bytes: 100 << 20,
    egress_mbps: 12.34, egress_pps: 2345, ingress_pps: 200, peers: 9, screen_shares: 1,
    estimate: { more_members: 87, basis: 'observed', scale: 1.25, per_member_millicores: 11.2, plan_cores: 2, status: 'ok', ...estimate },
    ...over,
  }
}

let wrapper: VueWrapper | null = null
beforeEach(() => { setLocale('en'); apiMock.mockReset() })
afterEach(() => { wrapper?.unmount(); wrapper = null; vi.useRealTimers() })

async function show(cap: ServerCapacityStatus) {
  apiMock.mockResolvedValue(cap)
  wrapper = mount(AdminCapacityPanel)
  await flushPromises()
  return wrapper
}

describe('AdminCapacityPanel', () => {
  it('shows the live figures and the estimate with its assumption line', async () => {
    const w = await show(fixture())
    expect(w.get('[data-testid="capacity-cpu"]').text()).toBe('0.46 of 4 cores')
    expect(w.get('[data-testid="capacity-memory"]').text()).toBe('300 MB of 1 GB')
    expect(w.get('[data-testid="capacity-media"]').text()).toContain('12.3 Mbit/s')
    expect(w.get('[data-testid="capacity-calls"]').text()).toBe('9 members · 1 screen share')
    expect(w.get('[data-testid="capacity-estimate"]').text()).toBe('Estimate: this server can take about 87 more members, each watching a screen share.')
    const assumption = w.get('[data-testid="capacity-assumption"]').text()
    expect(assumption).toContain('An estimate, not a promise')
    expect(assumption).toContain("measured CPU cost per packet (1.25 times the benchmark)")
    expect(assumption).toContain('half of the CPU (2 cores)')
  })

  it('says the benchmark values are used when there is too little traffic, and works with no traffic at all', async () => {
    const w = await show(fixture({ cpu_used_cores: 0, egress_mbps: 0, egress_pps: 0, ingress_pps: 0, peers: 0, screen_shares: 0, memory_limit_bytes: 0 }, { basis: 'table', scale: 1 }))
    expect(w.get('[data-testid="capacity-assumption"]').text()).toContain('benchmark values')
    expect(w.get('[data-testid="capacity-memory"]').text()).toBe('300 MB (no limit set)')
    expect(w.get('[data-testid="capacity-media"]').text()).toContain('0 Mbit/s')
  })

  it('says so when the server is busy or memory is tight, instead of a number', async () => {
    let w = await show(fixture({}, { status: 'busy', more_members: 0 }))
    expect(w.get('[data-testid="capacity-estimate"]').attributes('data-status')).toBe('busy')
    expect(w.get('[data-testid="capacity-estimate"]').text()).toContain('past half of its CPU')
    w.unmount()
    w = await show(fixture({}, { status: 'memory', more_members: 0 }))
    expect(w.get('[data-testid="capacity-estimate"]').text()).toContain('APP_MEM_LIMIT')
  })

  it('is German on request', async () => {
    setLocale('de')
    const w = await show(fixture())
    expect(w.text()).toContain('Kapazität')
    expect(w.get('[data-testid="capacity-estimate"]').text()).toContain('etwa 87 weitere Mitglieder aufnehmen')
  })

  it('refreshes every ten seconds and keeps the last figures when a refresh fails', async () => {
    vi.useFakeTimers()
    const w = await show(fixture())
    apiMock.mockRejectedValue(new Error('offline'))
    await vi.advanceTimersByTimeAsync(10_000)
    expect(apiMock).toHaveBeenCalledTimes(2)
    expect(w.get('[data-testid="capacity-cpu"]').text()).toBe('0.46 of 4 cores')
  })

  it('shows nothing before the first answer', () => {
    apiMock.mockReturnValue(new Promise(() => {}))
    wrapper = mount(AdminCapacityPanel)
    expect(wrapper.find('[data-testid="status-capacity"]').exists()).toBe(false)
  })

  it('uses singular forms in English and German', async () => {
    const one = fixture({ peers: 1, screen_shares: 1 }, { more_members: 1 })
    let w = await show(one)
    expect(w.get('[data-testid="capacity-calls"]').text()).toBe('1 member · 1 screen share')
    expect(w.get('[data-testid="capacity-estimate"]').text()).toContain('about 1 more member,')
    w.unmount()
    setLocale('de')
    w = await show(one)
    expect(w.get('[data-testid="capacity-calls"]').text()).toBe('1 Mitglied · 1 Bildschirmfreigabe')
    expect(w.get('[data-testid="capacity-estimate"]').text()).toContain('etwa 1 weiteres Mitglied')
  })

  it('says the figures are being collected right after the start, without numbers', async () => {
    const w = await show(fixture({ collecting: true }))
    expect(w.get('[data-testid="status-capacity-pending"]').text()).toContain('Collecting measurements')
    expect(w.find('[data-testid="capacity-estimate"]').exists()).toBe(false)
  })

  it('says so when the first load fails, and shows the figures once a later one works', async () => {
    vi.useFakeTimers()
    apiMock.mockRejectedValue(new Error('offline'))
    wrapper = mount(AdminCapacityPanel)
    await flushPromises()
    expect(wrapper.get('[data-testid="status-capacity-pending"]').text()).toContain('could not be loaded')
    apiMock.mockResolvedValue(fixture())
    await vi.advanceTimersByTimeAsync(10_000)
    expect(wrapper.find('[data-testid="capacity-estimate"]').exists()).toBe(true)
  })

  it('does not stack requests while one is still running', async () => {
    vi.useFakeTimers()
    apiMock.mockReturnValue(new Promise(() => {}))
    wrapper = mount(AdminCapacityPanel)
    await vi.advanceTimersByTimeAsync(35_000)
    expect(apiMock).toHaveBeenCalledTimes(1)
  })

  it('stops polling when it is closed, and pauses while the tab is hidden', async () => {
    vi.useFakeTimers()
    apiMock.mockResolvedValue(fixture())
    wrapper = mount(AdminCapacityPanel)
    await flushPromises()
    const hidden = vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('hidden')
    await vi.advanceTimersByTimeAsync(30_000)
    expect(apiMock).toHaveBeenCalledTimes(1)
    hidden.mockReturnValue('visible')
    document.dispatchEvent(new Event('visibilitychange'))
    await flushPromises()
    expect(apiMock).toHaveBeenCalledTimes(2)
    wrapper.unmount()
    wrapper = null
    await vi.advanceTimersByTimeAsync(60_000)
    document.dispatchEvent(new Event('visibilitychange'))
    expect(apiMock).toHaveBeenCalledTimes(2)
  })
})
