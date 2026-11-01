import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import ConnectionTest from './ConnectionTest.vue'
import { setLocale } from '../i18n'
import type { ConnectionReport } from '../lib/connectionVerdict'

const runMock = vi.fn()
vi.mock('../lib/connectionTest', () => ({ runConnectionTest: (...args: unknown[]) => runMock(...args) }))
vi.mock('../lib/connectionTestApi', () => ({ browserTestDeps: {} }))

const base: ConnectionReport = {
  candidates: { host: 2, srflx: 1, relay: 2 }, stunOffered: true, turnOffered: true,
  direct: { outcome: 'fail' }, relayUdp: { outcome: 'fail' }, relayTcp: { outcome: 'fail' }, voiceOff: false, limited: false
}

let wrapper: VueWrapper | null = null
beforeEach(() => { setLocale('en'); runMock.mockReset() })
afterEach(() => { wrapper?.unmount(); wrapper = null })

async function runWith(report: ConnectionReport) {
  runMock.mockResolvedValue(report)
  wrapper = mount(ConnectionTest)
  await wrapper.find('[data-testid="connection-test-run"]').trigger('click')
  await flushPromises()
  return wrapper
}

describe('ConnectionTest', () => {
  it('explains itself and shows no result before it ran', () => {
    wrapper = mount(ConnectionTest)
    expect(wrapper.text()).toContain('Test my connection')
    expect(wrapper.text()).toContain('needs no microphone')
    expect(wrapper.find('[data-testid="connection-test-result"]').exists()).toBe(false)
  })

  it('says the direct connection works', async () => {
    const w = await runWith({ ...base, direct: { outcome: 'ok', pair: { localType: 'srflx', remoteType: 'host', remoteAddress: '203.0.113.7' } } })
    expect(w.get('[data-testid="connection-test-verdict"]').text()).toBe('Direct connection works')
    expect(w.get('[data-testid="connection-test-verdict"]').attributes('data-kind')).toBe('direct')
    expect(w.find('[data-testid="connection-test-fix"]').exists()).toBe(false)
    expect(w.get('[data-testid="connection-test-run"]').text()).toBe('Run it again')
  })

  it('says only the relay works, with the cause, the fix and a link to the troubleshooting guide', async () => {
    const w = await runWith({ ...base, relayUdp: { outcome: 'ok' }, relayTcp: { outcome: 'ok' } })
    expect(w.get('[data-testid="connection-test-verdict"]').text()).toBe('Only the relay works (direct UDP blocked or not forwarded)')
    expect(w.get('[data-testid="connection-test-cause"]').attributes('data-cause')).toBe('udp_not_forwarded')
    expect(w.get('[data-testid="connection-test-fix"]').text()).toContain('Forward the voice UDP ports')
    expect(w.get('a').attributes('href')).toContain('/doc/troubleshooting-voice.md')
    expect(w.get('[data-testid="connection-test-path-direct"]').attributes('data-outcome')).toBe('fail')
    expect(w.get('[data-testid="connection-test-path-relayUdp"]').text()).toBe('Relay over UDP: Works')
    expect(w.get('[data-testid="connection-test-addresses"]').text()).toBe('Addresses found: 2 local, 1 public, 2 relay')
  })

  it('says nothing works', async () => {
    const w = await runWith({ ...base, candidates: { host: 1, srflx: 0, relay: 0 } })
    expect(w.get('[data-testid="connection-test-verdict"]').text()).toBe('Nothing works')
    expect(w.get('[data-testid="connection-test-cause"]').attributes('data-cause')).toBe('only_local_addresses')
  })

  it('shows a progress line while running and the verdict in German', async () => {
    setLocale('de')
    let finish: (r: ConnectionReport) => void = () => {}
    runMock.mockImplementation((_deps: unknown, onStep: (s: string) => void) => {
      onStep('relayTcp')
      return new Promise<ConnectionReport>(resolve => { finish = resolve })
    })
    wrapper = mount(ConnectionTest)
    await wrapper.get('[data-testid="connection-test-run"]').trigger('click')
    expect(wrapper.get('[data-testid="connection-test-step"]').text()).toBe('Probiere das Relay über TCP …')
    expect(wrapper.get('[data-testid="connection-test-run"]').attributes('disabled')).toBeDefined()
    finish({ ...base, direct: { outcome: 'ok', pair: { localType: 'host', remoteType: 'host', remoteAddress: '10.0.0.2' } } })
    await flushPromises()
    expect(wrapper.get('[data-testid="connection-test-verdict"]').text()).toBe('Direkte Verbindung funktioniert')
    expect(wrapper.get('[data-testid="connection-test-cause"]').attributes('data-cause')).toBe('same_network')
  })

  it('says so when the test itself could not run', async () => {
    runMock.mockRejectedValue(new Error('boom'))
    wrapper = mount(ConnectionTest)
    await wrapper.get('[data-testid="connection-test-run"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="connection-test-failed"]').text()).toBe('The test could not run.')
  })

  it('can hide its own heading when the surrounding section has one', () => {
    wrapper = mount(ConnectionTest, { props: { heading: false } })
    expect(wrapper.find('h4').exists()).toBe(false)
  })

  it('shows each verdict with its own tone and icon, and the inconclusive one without a fix of its own cause', async () => {
    const cases: Array<[ConnectionReport, string, string]> = [
      [{ ...base, direct: { outcome: 'ok' } }, 'direct', 'text-mnema-success'],
      [{ ...base, relayTcp: { outcome: 'ok' } }, 'relay_only', 'text-mnema-warning'],
      [base, 'none', 'text-mnema-danger'],
      [{ ...base, limited: true }, 'inconclusive', 'text-mnema-muted']
    ]
    for (const [report, kind, tone] of cases) {
      const w = await runWith(report)
      const verdict = w.get('[data-testid="connection-test-verdict"]')
      expect(verdict.attributes('data-kind'), kind).toBe(kind)
      expect(verdict.classes(), kind).toContain(tone)
      expect(verdict.find('svg').exists(), kind).toBe(true)
      w.unmount()
      wrapper = null
    }
  })

  it('shows the fix only for causes that have one', async () => {
    const w = await runWith({ ...base, direct: { outcome: 'ok', pair: { localType: 'host', remoteType: 'host', remoteAddress: '8.8.8.8' } } })
    expect(w.find('[data-testid="connection-test-fix"]').exists()).toBe(false)
    await w.get('[data-testid="connection-test-run"]').trigger('click')
    await flushPromises()
    runMock.mockResolvedValue({ ...base, limited: true })
    await w.get('[data-testid="connection-test-run"]').trigger('click')
    await flushPromises()
    expect(w.get('[data-testid="connection-test-fix"]').text()).toContain('Wait a few minutes')
  })

  it('clears an earlier result while a new test runs', async () => {
    const w = await runWith({ ...base, direct: { outcome: 'ok' } })
    runMock.mockReturnValue(new Promise(() => {}))
    await w.get('[data-testid="connection-test-run"]').trigger('click')
    expect(w.find('[data-testid="connection-test-result"]').exists()).toBe(false)
    expect(w.get('[data-testid="connection-test-run"]').text()).toBe('Testing…')
  })

  it('ignores a result that arrives after it was closed', async () => {
    let finish: (r: ConnectionReport) => void = () => {}
    runMock.mockReturnValue(new Promise<ConnectionReport>(resolve => { finish = resolve }))
    wrapper = mount(ConnectionTest)
    await wrapper.get('[data-testid="connection-test-run"]').trigger('click')
    wrapper.unmount()
    wrapper = null
    finish(base)
    await flushPromises()
  })
})

describe('the upload hint', () => {
  const direct = { outcome: 'ok' as const, pair: { localType: 'srflx', remoteType: 'host', remoteAddress: '203.0.113.7' } }

  it('says what the download means for the screen share size, and what it measured', async () => {
    const w = await runWith({ ...base, direct, speed: { outcome: 'ok', mbps: 100 } })
    const text = w.get('[data-testid="connection-test-speed"]').text()
    expect(text).toContain('100 Mbit/s')
    expect(text).toContain('nearest reverse proxy')
    expect(text).toContain('roughly its upload')
    expect(text).not.toContain('at least')
    expect(text).toContain('about 51 members (1.5 Mbit/s plus 30 % overhead each)')
    expect(text).toContain('your own connection can be the limit')
  })

  it('uses the singular for one watcher and says so when the upload is not enough for even one', async () => {
    let w = await runWith({ ...base, direct, speed: { outcome: 'ok', mbps: 2.5 } })
    expect(w.get('[data-testid="connection-test-speed"]').text()).toContain('about 1 member (')
    w.unmount()
    w = await runWith({ ...base, direct, speed: { outcome: 'ok', mbps: 1 } })
    expect(w.get('[data-testid="connection-test-speed"]').text()).toContain('not enough upload for even one member')
  })

  it('keeps the number and the members estimate for a very fast download, with the note that it may be local', async () => {
    const w = await runWith({ ...base, direct, speed: { outcome: 'ok', mbps: 940, maybeLocal: true } })
    const text = w.get('[data-testid="connection-test-speed"]').text()
    expect(text).toContain('940 Mbit/s')
    expect(text).toContain('about 482 members')
    expect(text).toContain('This may be a local connection; test from outside to be sure.')
    w.unmount()
    const plain = await runWith({ ...base, direct, speed: { outcome: 'ok', mbps: 100 } })
    expect(plain.get('[data-testid="connection-test-speed"]').text()).not.toContain('local connection')
  })

  it('says it cannot measure the upload from the same network', async () => {
    const w = await runWith({ ...base, direct, speed: { outcome: 'lan' } })
    expect(w.get('[data-testid="connection-test-speed"]').text()).toContain('cannot measure the server’s upload')
  })

  it('has its own words for a failed, a refused and a skipped speed test, and shows nothing when none was made', async () => {
    const texts: Record<string, string> = {}
    for (const outcome of ['fail', 'limited', 'busy'] as const) {
      const w = await runWith({ ...base, direct, speed: { outcome } })
      texts[outcome] = w.get('[data-testid="connection-test-speed"]').text()
      w.unmount()
      wrapper = null
    }
    expect(texts.fail).toContain('could not run')
    expect(texts.limited).toContain('Too many speed tests')
    expect(texts.busy).toContain('a Talk is running')
    const w = await runWith({ ...base, direct })
    expect(w.find('[data-testid="connection-test-speed"]').exists()).toBe(false)
  })

  it('stops the speed test when the dialog is closed', async () => {
    let seen: AbortSignal | undefined
    runMock.mockImplementation((_d: unknown, _s: unknown, signal: AbortSignal) => { seen = signal; return new Promise(() => {}) })
    wrapper = mount(ConnectionTest)
    await wrapper.get('[data-testid="connection-test-run"]').trigger('click')
    expect(seen?.aborted).toBe(false)
    wrapper.unmount()
    wrapper = null
    expect(seen?.aborted).toBe(true)
  })
})
