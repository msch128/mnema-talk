import type { Page } from '@playwright/test'
import { writeFileSync } from 'node:fs'
import { apiFetch, trackPeerConnections, expectTalkLive, leaveTalks } from './helpers'
import { test, expect } from './fixtures'
import { installPerfProbe } from './perf-probe'
import type { ProbeSnapshot } from './perf-probe'

// What an open Mnema costs while nobody is using it: the app signed in on a
// text channel, with the tab visible and hidden (a locked phone, a minimised
// window), after leaving a call, and in a call with the tab hidden. The numbers
// are counted per minute from the page's own timers, animation frames, requests
// and socket frames plus CDP's Performance metrics, and the hidden ones must
// stay under a budget so a polling loop that forgets visibility fails here.
// PERF_WINDOW_S sets the length of each window (default 30 s), PERF_OUT writes
// the numbers as JSON, PERF_NO_ASSERT=1 only measures.

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'cool_guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const WINDOW_S = Number(process.env.PERF_WINDOW_S ?? 30)
const OUT = process.env.PERF_OUT
const ASSERT = process.env.PERF_NO_ASSERT !== '1'
const suffix = Date.now().toString(36)

interface Row {
  state: string
  windowS: number
  timersPerMin: number
  rafPerMin: number
  requestsPerMin: number
  wsFramesPerMin: number
  cpuMsPerMin: number
  scriptMsPerMin: number
  layoutsPerMin: number
  styleRecalcsPerMin: number
  liveIntervals: number
  audioContexts: string[]
  liveTracks: number
  pageWorkers: number
  targetWorkers: number
  topTimers: string[]
  wsTypes: string[]
  topMutations: string[]
}

// Budgets for the hidden window. A hidden tab may keep its socket alive and
// nothing else: a ping now and then, no polling, no frames, no layout.
const HIDDEN_BUDGET = { timersPerMin: 30, rafPerMin: 0, requestsPerMin: 4, wsFramesPerMin: 40, layoutsPerMin: 10 }

async function signIn(page: Page) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('Benutzername').fill(ADMIN_USER)
  await dialog.getByLabel('Passwort').fill(ADMIN_PASSWORD)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

const snapshot = (page: Page) => page.evaluate(() => (window as unknown as { __perfSnapshot: () => ProbeSnapshot }).__perfSnapshot())
const setHidden = (page: Page, hidden: boolean) => page.evaluate((h) => (window as unknown as { __perfSetHidden: (h: boolean) => void }).__perfSetHidden(h), hidden)

test('an open app is idle when nobody uses it', async ({ browser }) => {
  test.setTimeout((WINDOW_S * 4 + 120) * 1000)
  const context = await browser.newContext({ permissions: ['microphone', 'camera'] })
  await context.addInitScript(trackPeerConnections)
  // The real noise filter, so a worker or worklet left behind by a call shows up.
  await context.addInitScript(() => localStorage.setItem('mnema_noise', 'ai'))
  await context.addInitScript(installPerfProbe)
  const page = await context.newPage()
  const cdp = await context.newCDPSession(page)
  const browserCdp = await browser.newBrowserCDPSession()
  await cdp.send('Performance.enable')

  let requests = 0
  page.on('request', () => { requests++ })

  const metrics = async () => {
    const { metrics } = await cdp.send('Performance.getMetrics')
    return Object.fromEntries(metrics.map((m) => [m.name, m.value]))
  }
  const workerTargets = async () => {
    const { targetInfos } = await browserCdp.send('Target.getTargets')
    return targetInfos.filter((t) => t.type === 'worker' || t.type === 'service_worker' || t.type === 'shared_worker').length
  }

  const rows: Row[] = []
  async function measure(state: string) {
    const [s0, m0, r0] = [await snapshot(page), await metrics(), requests]
    const started = Date.now()
    await page.waitForTimeout(WINDOW_S * 1000)
    const [s1, m1, r1] = [await snapshot(page), await metrics(), requests]
    const minutes = (Date.now() - started) / 60_000
    const per = (n: number) => Math.round((n / minutes) * 10) / 10
    const d = (k: string) => (m1[k] ?? 0) - (m0[k] ?? 0)
    const before = new Map(s0.sites.map((s) => [`${s.delay}|${s.site}`, s.fires]))
    const top = s1.sites
      .map((s) => ({ s, n: s.fires - (before.get(`${s.delay}|${s.site}`) ?? 0) }))
      .filter((x) => x.n > 0).sort((a, b) => b.n - a.n).slice(0, 6)
      .map((x) => `${per(x.n)}/min every ${x.s.delay}ms ${x.s.site}`)
    const delta = (a: Record<string, number>, b: Record<string, number>) => Object.entries(b)
      .map(([k, n]) => [k, n - (a[k] ?? 0)] as const).filter(([, n]) => n > 0).sort((x, y) => y[1] - x[1]).slice(0, 6)
      .map(([k, n]) => `${per(n)}/min ${k}`)
    const row: Row = {
      state, windowS: WINDOW_S,
      timersPerMin: per(s1.intervalFires + s1.timeoutFires - s0.intervalFires - s0.timeoutFires),
      rafPerMin: per(s1.rafFires - s0.rafFires),
      requestsPerMin: per(r1 - r0),
      wsFramesPerMin: per(s1.wsSent + s1.wsReceived - s0.wsSent - s0.wsReceived),
      cpuMsPerMin: per(d('TaskDuration') * 1000),
      scriptMsPerMin: per(d('ScriptDuration') * 1000),
      layoutsPerMin: per(d('LayoutCount')),
      styleRecalcsPerMin: per(d('RecalcStyleCount')),
      liveIntervals: s1.liveIntervals, audioContexts: s1.audioContexts, liveTracks: s1.liveTracks,
      pageWorkers: s1.liveWorkers, targetWorkers: await workerTargets(), topTimers: top, wsTypes: delta(s0.wsTypes, s1.wsTypes), topMutations: delta(s0.mutations, s1.mutations),
    }
    rows.push(row)
    return row
  }

  await signIn(page)
  const text = `Idle ${suffix}`
  const voice = `IdleTalk ${suffix}`
  expect((await apiFetch(page, 'POST', '/api/admin/channels', { name: text, type: 'text' })).status).toBe(201)
  expect((await apiFetch(page, 'POST', '/api/admin/channels', { name: voice, type: 'voice' })).status).toBe(201)
  await page.reload()
  await page.locator('[data-channel-type="text"]', { hasText: text }).click()
  await expect(page.getByRole('textbox').first()).toBeVisible()
  await page.waitForTimeout(3000)

  await measure('a. text channel, tab visible')
  await setHidden(page, true)
  await page.waitForTimeout(1500)
  const hidden = await measure('b. text channel, tab hidden')
  await setHidden(page, false)

  await page.locator('[data-channel-type="voice"]', { hasText: voice }).click()
  await expectTalkLive(page)
  await page.waitForTimeout(3000)
  await setHidden(page, true)
  await page.waitForTimeout(1500)
  const inCall = await measure('d. in a call, tab hidden')
  await setHidden(page, false)
  await leaveTalks([page])
  await page.locator('[data-channel-type="text"]', { hasText: text }).click()
  await page.waitForTimeout(3000)
  await setHidden(page, true)
  await page.waitForTimeout(1500)
  const left = await measure('c. after leaving the call, tab hidden')

  if (OUT) writeFileSync(OUT, JSON.stringify(rows, null, 2))
  console.log(JSON.stringify(rows, null, 2))

  if (ASSERT) {
    for (const row of [hidden, left]) {
      expect.soft(row.timersPerMin, `${row.state}: timers ${row.topTimers.join(' | ')}`).toBeLessThanOrEqual(HIDDEN_BUDGET.timersPerMin)
      expect.soft(row.rafPerMin, `${row.state}: animation frames`).toBeLessThanOrEqual(HIDDEN_BUDGET.rafPerMin)
      expect.soft(row.requestsPerMin, `${row.state}: HTTP requests`).toBeLessThanOrEqual(HIDDEN_BUDGET.requestsPerMin)
      expect.soft(row.wsFramesPerMin, `${row.state}: socket frames`).toBeLessThanOrEqual(HIDDEN_BUDGET.wsFramesPerMin)
      expect.soft(row.layoutsPerMin, `${row.state}: layouts`).toBeLessThanOrEqual(HIDDEN_BUDGET.layoutsPerMin)
    }
    // In a call the voice gate reads the microphone level 16 times a second and that is all the page does:
    // no rings drawn for nobody, no stats polling at the visible pace, no video pulled for a hidden tab.
    expect.soft(inCall.timersPerMin, `${inCall.state}: timers ${inCall.topTimers.join(' | ')}`).toBeLessThanOrEqual(1100)
    expect.soft(inCall.layoutsPerMin, `${inCall.state}: layouts`).toBeLessThanOrEqual(10)
    expect.soft(inCall.styleRecalcsPerMin, `${inCall.state}: style recalculations ${inCall.topMutations.join(' | ')}`).toBeLessThanOrEqual(60)
    expect.soft(inCall.requestsPerMin, `${inCall.state}: HTTP requests`).toBeLessThanOrEqual(HIDDEN_BUDGET.requestsPerMin)
    // Leaving a call leaves nothing running: no live microphone, no suspended-or-running audio graph, no worker.
    expect.soft(left.liveTracks, 'microphone tracks after the call').toBe(0)
    expect.soft(left.audioContexts.filter((s) => s === 'running'), 'audio contexts still running after the call').toEqual([])
    expect.soft(left.targetWorkers, 'workers after the call').toBeLessThanOrEqual(1)
    expect.soft(left.pageWorkers, 'page workers after the call').toBe(0)
  }
  await context.close()
})
