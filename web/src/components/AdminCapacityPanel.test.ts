import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import type { ApiOptions } from '../lib/api'
import AdminCapacityPanel from './AdminCapacityPanel.vue'
import { setLocale } from '../i18n'
import type { ServerCapacityScenario, ServerCapacityStatus } from '../types/rest'

const apiMock = vi.fn<(path: string, options?: ApiOptions) => Promise<unknown>>()
vi.mock('../lib/api', async original => ({
  ...(await original<typeof import('../lib/api')>()),
  api: (path: string, options?: ApiOptions) => apiMock(path, options),
}))

const row = (id: ServerCapacityScenario['id'], members: number, limit: ServerCapacityScenario['limit'], upload_mbps: number, over: Partial<ServerCapacityScenario> = {}): ServerCapacityScenario =>
  ({ id, members, limit, upload_mbps, cpu_cores: 1.9, memory_bytes: 600e6, basis: 'measured', measured_up_to: 100, extrapolated: false, ...over })

const SCENARIOS: ServerCapacityScenario[] = [
  row('chat', 1771, 'cpu', 0, { measured_up_to: 1000, extrapolated: true }),
  row('broadcast', 453, 'memory', 70.6, { extrapolated: true }),
  row('conversation', 40, 'cpu', 81.1, { basis: 'derived', measured_up_to: 0, extrapolated: true }),
  row('screen', 87, 'cpu', 182.4),
  row('video', 20, 'upload', 266.8, { basis: 'derived', measured_up_to: 0, extrapolated: true }),
]

function fixture(over: Partial<ServerCapacityStatus> = {}, estimate: Partial<ServerCapacityStatus['estimate']> = {}): ServerCapacityStatus {
  return {
    collecting: false, cpu_cores: 4, cpu_used_cores: 0.46, memory_limit_bytes: 1 << 30, rss_bytes: 300 << 20, heap_bytes: 100 << 20,
    egress_mbps: 12.34, egress_pps: 2345, ingress_pps: 200, peers: 9, screen_shares: 1,
    estimate: { more_members: 87, basis: 'observed', scale: 1.25, per_member_millicores: 11.2, plan_cores: 2, status: 'ok', upload_limit_mbps: 0, scenarios: SCENARIOS, ...estimate },
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
    expect(w.get('[data-testid="capacity-estimate"]').text()).toBe('With what is free now, one Talk can hold about:')
    const assumption = w.get('[data-testid="capacity-assumption"]')
    expect(assumption.text()).toContain('An estimate, not a promise')
    expect(assumption.text()).toContain("measured CPU cost per packet (1.25 times the benchmark)")
    expect(assumption.text()).toContain('half of the CPU (2 cores)')
    expect(assumption.text()).toMatch(/Details: doc\/benchmark\.md\.$/)
    expect(assumption.get('a').attributes('href')).toMatch(/\/blob\/main\/doc\/benchmark\.md#sizing-guide$/)
  })

  it('lists one row per scenario with the people, the limit, the upload and how sure the figure is', async () => {
    const w = await show(fixture())
    expect(w.findAll('[data-testid="capacity-scenarios"] > li').map(li => li.attributes('data-testid'))).toEqual(
      ['chat', 'broadcast', 'conversation', 'screen', 'video'].map(id => `capacity-scenario-${id}`))
    const screen = w.get('[data-testid="capacity-scenario-screen"]')
    expect(screen.text()).toContain('Screen share – 1 share and 3 speakers, everyone else watches')
    expect(screen.get('[data-testid="capacity-scenario-people"]').text()).toBe('about 87')
    expect(screen.get('[data-testid="capacity-scenario-cost"]').text()).toBe('limited by CPU · 182 Mbit/s upload')
    expect(screen.get('[data-testid="capacity-scenario-basis"]').text()).toBe('measured up to 100')
    const chat = w.get('[data-testid="capacity-scenario-chat"]')
    expect(chat.get('[data-testid="capacity-scenario-people"]').text()).toBe('about 1,771')
    expect(chat.get('[data-testid="capacity-scenario-cost"]').text()).toBe('limited by CPU')
    expect(chat.get('[data-testid="capacity-scenario-basis"]').text()).toBe('beyond the largest measured run (1,000)')
    expect(w.get('[data-testid="capacity-scenario-broadcast"] [data-testid="capacity-scenario-cost"]').text()).toBe('limited by memory · 71 Mbit/s upload')
    expect(w.get('[data-testid="capacity-scenario-video"] [data-testid="capacity-scenario-basis"]').text()).toBe('derived from measured costs')
    expect(w.get('[data-testid="capacity-scenario-video"] [data-testid="capacity-scenario-cost"]').text()).toContain('limited by upload')
    expect(w.get('[data-testid="capacity-upload-unknown"]').text()).toContain('VOICE_UPLOAD_LIMIT_MBIT')
  })

  it('does not ask for the upload limit once it is set', async () => {
    const w = await show(fixture({}, { upload_limit_mbps: 40 }))
    expect(w.find('[data-testid="capacity-upload-unknown"]').exists()).toBe(false)
  })

  it('says the benchmark values are used when there is too little traffic, and works with no traffic at all', async () => {
    const w = await show(fixture({ cpu_used_cores: 0, egress_mbps: 0, egress_pps: 0, ingress_pps: 0, peers: 0, screen_shares: 0, memory_limit_bytes: 0 }, { basis: 'table', scale: 1 }))
    expect(w.get('[data-testid="capacity-assumption"]').text()).toContain('benchmark values')
    expect(w.get('[data-testid="capacity-memory"]').text()).toBe('300 MB (no limit set)')
    expect(w.get('[data-testid="capacity-media"]').text()).toContain('0 Mbit/s')
  })

  it('says so when the server is busy or memory is tight, instead of a number', async () => {
    let w = await show(fixture({}, { status: 'busy', more_members: 0, scenarios: [] }))
    expect(w.get('[data-testid="capacity-estimate"]').attributes('data-status')).toBe('busy')
    expect(w.get('[data-testid="capacity-estimate"]').text()).toContain('past half of its CPU')
    expect(w.find('[data-testid="capacity-scenarios"]').exists()).toBe(false)
    expect(w.find('[data-testid="capacity-upload-unknown"]').exists()).toBe(false)
    w.unmount()
    w = await show(fixture({}, { status: 'memory', more_members: 0, scenarios: [] }))
    expect(w.get('[data-testid="capacity-estimate"]').text()).toContain('APP_MEM_LIMIT')
  })

  it('is German on request', async () => {
    setLocale('de')
    const w = await show(fixture())
    expect(w.text()).toContain('Kapazität')
    expect(w.get('[data-testid="capacity-estimate"]').text()).toBe('Mit dem, was gerade frei ist, fasst ein Talk etwa:')
    expect(w.get('[data-testid="capacity-scenario-screen"]').text()).toContain('Bildschirmfreigabe')
    expect(w.get('[data-testid="capacity-scenario-chat"] [data-testid="capacity-scenario-people"]').text()).toBe('etwa 1.771')
    expect(w.get('[data-testid="capacity-scenario-broadcast"] [data-testid="capacity-scenario-cost"]').text()).toBe('begrenzt durch Speicher · 71 Mbit/s Upload')
    expect(w.get('[data-testid="capacity-assumption"]').text()).toContain('Einzelheiten: doc/benchmark.md.')
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
    const one = fixture({ peers: 1, screen_shares: 1 })
    let w = await show(one)
    expect(w.get('[data-testid="capacity-calls"]').text()).toBe('1 member · 1 screen share')
    w.unmount()
    setLocale('de')
    w = await show(one)
    expect(w.get('[data-testid="capacity-calls"]').text()).toBe('1 Mitglied · 1 Bildschirmfreigabe')
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
