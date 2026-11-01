import type { Page } from '@playwright/test'
import { apiFetch, responseString, suiteMember, trackPeerConnections, leaveTalks, audioDecodedIn } from './helpers'
import { test, expect } from './fixtures'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'cool_guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
const USER = suiteMember.username
const USER_PASSWORD = suiteMember.password
const VOICE_CHANNEL = `Late ${suffix}`



// Observe the real production filter without mocking assets, PCM or transfer
// ports. Received RTP alone could also pass after browser-filter fallback.
const trackAudioFilters = () => {
  localStorage.setItem('mnema_noise', 'ai')
  // The fake microphone is a steady tone that Opus DTX would take for silence; this spec listens for it.
  localStorage.setItem('mnema_dtx', 'false')
  window.__dfnWorkers = []
  window.__dfnNodes = []
  const NativeWorker = window.Worker
  window.Worker = class extends NativeWorker {
    constructor(...args: ConstructorParameters<typeof Worker>) {
      super(...args)
      if (!String(args[0]).includes('dfnWorker')) return
      const state = { ready: false, failed: false, why: '' }
      window.__dfnWorkers.push(state)
      this.addEventListener('message', ({ data }) => {
        if (data.type === 'ready') state.ready = true
        if (data.type === 'failed') { state.failed = true; state.why = JSON.stringify(data) }
      })
      this.addEventListener('error', (e) => { state.failed = true; state.why = `error: ${e.message}` })
    }
  }
  const NativeNode = window.AudioWorkletNode
  window.AudioWorkletNode = class extends NativeNode {
    constructor(...args: ConstructorParameters<typeof AudioWorkletNode>) {
      super(...args)
      if (args[1] !== 'deepfilter-worker-bridge') return
      const state = { node: this, failed: false, why: '' }
      window.__dfnNodes.push(state)
      this.port.addEventListener('message', ({ data }) => {
        if (data.type === 'failed') { state.failed = true; state.why = JSON.stringify(data) }
      })
      this.addEventListener('processorerror', () => { state.failed = true; state.why = 'processorerror' })
    }
  }
}

const filterState = (page: Page) => page.evaluate(() => ({
  ready: window.__dfnWorkers.at(-1)?.ready ?? false,
  failed: window.__dfnWorkers.some(w => w.failed) || window.__dfnNodes.some(n => n.failed),
  // What the filter said when it gave up, so a failure explains itself.
  why: [...window.__dfnWorkers, ...window.__dfnNodes].filter(x => x.failed).map(x => x.why),
  processed: window.__dfnNodes.at(-1)?.node.filterStats?.processedFrames ?? 0,
}))

// Audio bytes received on the current connection, from all other members.
const audioBytesIn = (page: Page) => page.evaluate(async () => {
  const pc = window.__pcs.at(-1)
  if (!pc) return 0
  let bytes = 0
  ;(await pc.getStats()).forEach((s) => { if (s.type === 'inbound-rtp' && s.kind === 'audio') bytes += s.bytesReceived || 0 })
  return bytes
})



async function signIn(page: Page, username: string, password: string) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('Benutzername').fill(username)
  await dialog.getByLabel('Passwort').fill(password)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

// The invite-only registration allows ten sign-ups an hour from one address,
// which the whole suite shares, so the second test reuses the first one's member.
test.describe.configure({ mode: 'serial' })

// Someone joins a Talk where another member is already talking: both must
// hear each other (regression: the late joiner's microphone was never sent).
test('a member joining an occupied Talk is heard and hears', async ({ browser }) => {
  const firstCtx = await browser.newContext({ permissions: ['microphone', 'camera'] })
  const lateCtx = await browser.newContext({ permissions: ['microphone', 'camera'] })
  await firstCtx.addInitScript(trackPeerConnections)
  await lateCtx.addInitScript(trackPeerConnections)
  await firstCtx.addInitScript(trackAudioFilters)
  await lateCtx.addInitScript(trackAudioFilters)
  const first = await firstCtx.newPage()
  const late = await lateCtx.newPage()

  await first.goto('/')
  const dialog = first.getByRole('dialog')
  await dialog.getByLabel('Benutzername').fill(ADMIN_USER)
  await dialog.getByLabel('Passwort').fill(ADMIN_PASSWORD)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
  expect((await apiFetch(first, 'POST', '/api/admin/channels', { name: VOICE_CHANNEL, type: 'voice' })).status).toBe(201)
  if (suiteMember.registered) {
    await signIn(late, USER, USER_PASSWORD)
  } else {
    const invite = await apiFetch(first, 'POST', '/api/admin/invites', { max_uses: 1 })
    await late.goto('/')
    expect((await apiFetch(late, 'POST', '/api/auth/register', { username: USER, password: USER_PASSWORD, invite_code: responseString(invite, 'code') })).status).toBe(201)
    suiteMember.registered = true
  }
  await first.reload()
  await late.reload()

  const sidebarVoice = (page: Page) => page.locator('[data-channel-type="voice"]', { hasText: VOICE_CHANNEL })
  await sidebarVoice(first).click()
  await expect.poll(() => first.evaluate(() => window.__pcs.at(-1)?.connectionState)).toBe('connected')
  await expect.poll(async () => (await filterState(first)).processed, { timeout: 15_000 }).toBeGreaterThan(0)
  const beforeLateJoin = (await filterState(first)).processed

  // The second member joins the occupied Talk.
  await sidebarVoice(late).click()
  await expect.poll(() => late.evaluate(() => window.__pcs.at(-1)?.connectionState)).toBe('connected')

  // Audio flows both ways (the fake microphone sends a tone).
  for (const page of [first, late]) {
    await expect.poll(() => audioBytesIn(page), { timeout: 15_000 }).toBeGreaterThan(2000)
    await expect.poll(async () => (await filterState(page)).processed, { timeout: 15_000 }).toBeGreaterThan(0)
    expect((await filterState(page)).ready).toBe(true)
    const state = await filterState(page)
    expect({ failed: state.failed, why: state.why }).toEqual({ failed: false, why: [] })
  }
  // The first microphone keeps processing after the other member starts its
  // own filter; counts reflect accepted PCM replies from the actual Worker.
  await expect.poll(async () => (await filterState(first)).processed, { timeout: 15_000 }).toBeGreaterThan(beforeLateJoin)

  // Both see the same safety code for the keys they share, naming both of them.
  const safetyCode = async (page: Page) => {
    await page.getByTestId('voice-panel-ping').click()
    const code = page.getByTestId('media-safety-code')
    await expect(code).toHaveText(/^\d{5} \d{5} \d{5} \d{5}$/, { timeout: 15_000 })
    await expect(page.getByTestId('media-verification').locator('[data-in-room="true"]')).toHaveCount(2)
    return code.textContent()
  }
  await expect.poll(async () => {
    const codes = [await safetyCode(first), await safetyCode(late)]
    for (const page of [first, late]) await page.keyboard.press('Escape')
    return codes[0] === codes[1]
  }, { timeout: 30_000 }).toBe(true)

  await leaveTalks([first, late])
  await firstCtx.close()
  await lateCtx.close()
})

// Either member may have the lowest id, which the group treats differently, so
// both reload in turn: the group must take each of them back inside the grace
// period, and they must hear each other again.
test('a member who reloads inside the grace period is heard again and hears again', async ({ browser }) => {
  const firstCtx = await browser.newContext({ permissions: ['microphone', 'camera'] })
  const secondCtx = await browser.newContext({ permissions: ['microphone', 'camera'] })
  await firstCtx.addInitScript(trackPeerConnections)
  await secondCtx.addInitScript(trackPeerConnections)
  const first = await firstCtx.newPage()
  const second = await secondCtx.newPage()
  await signIn(first, ADMIN_USER, ADMIN_PASSWORD)
  await signIn(second, USER, USER_PASSWORD)
  const channel = `Reload ${suffix}`
  expect((await apiFetch(first, 'POST', '/api/admin/channels', { name: channel, type: 'voice' })).status).toBe(201)
  await first.reload()
  await second.reload()

  const sidebarVoice = (page: Page) => page.locator('[data-channel-type="voice"]', { hasText: channel })
  await sidebarVoice(first).click()
  await sidebarVoice(second).click()
  const connected = (page: Page) => expect.poll(() => page.evaluate(() => window.__pcs.at(-1)?.connectionState), { timeout: 20_000 }).toBe('connected')
  const hearEachOther = async () => {
    for (const page of [first, second]) await expect.poll(() => audioDecodedIn(page), { timeout: 20_000 }).toBeGreaterThan(24_000)
  }
  await connected(first)
  await connected(second)
  await hearEachOther()

  for (const reloading of [first, second]) {
    const other = reloading === first ? second : first
    const before = await other.evaluate(() => window.__pcs.length)
    await reloading.reload()
    // The reload rejoins the Talk by itself, well inside the grace period.
    await connected(reloading)
    expect(await other.evaluate(() => window.__pcs.length)).toBe(before)
    await hearEachOther()
  }

  await leaveTalks([first, second])
  await firstCtx.close()
  await secondCtx.close()
})
