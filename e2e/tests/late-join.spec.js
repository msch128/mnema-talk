import { randomBytes } from 'node:crypto'
import { test, expect } from '@playwright/test'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'Herzog'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
const USER = `late_${suffix}`
const USER_PASSWORD = `pw-${suffix}-${randomBytes(8).toString('hex')}`
const VOICE_CHANNEL = `Late ${suffix}`

const trackPeerConnections = () => {
  const Native = window.RTCPeerConnection
  window.__pcs = []
  window.RTCPeerConnection = function (...args) {
    const pc = new Native(...args)
    window.__pcs.push(pc)
    return pc
  }
  window.RTCPeerConnection.prototype = Native.prototype
  window.RTCPeerConnection.generateCertificate = Native.generateCertificate
}

// Observe the real production filter without mocking assets, PCM or transfer
// ports. Received RTP alone could also pass after browser-filter fallback.
const trackAudioFilters = () => {
  localStorage.setItem('mnema_noise', 'ai')
  window.__dfnWorkers = []
  window.__dfnNodes = []
  const NativeWorker = window.Worker
  window.Worker = class extends NativeWorker {
    constructor(...args) {
      super(...args)
      if (!String(args[0]).includes('dfnWorker')) return
      const state = { ready: false, failed: false }
      window.__dfnWorkers.push(state)
      this.addEventListener('message', ({ data }) => {
        if (data.type === 'ready') state.ready = true
        if (data.type === 'failed') state.failed = true
      })
      this.addEventListener('error', () => { state.failed = true })
    }
  }
  const NativeNode = window.AudioWorkletNode
  window.AudioWorkletNode = class extends NativeNode {
    constructor(...args) {
      super(...args)
      if (args[1] !== 'deepfilter-worker-bridge') return
      const state = { node: this, failed: false }
      window.__dfnNodes.push(state)
      this.port.addEventListener('message', ({ data }) => {
        if (data.type === 'failed') state.failed = true
      })
      this.addEventListener('processorerror', () => { state.failed = true })
    }
  }
}

const filterState = (page) => page.evaluate(() => ({
  ready: window.__dfnWorkers.at(-1)?.ready ?? false,
  failed: window.__dfnWorkers.some(w => w.failed) || window.__dfnNodes.some(n => n.failed),
  processed: window.__dfnNodes.at(-1)?.node.filterStats?.processedFrames ?? 0,
}))

// Audio bytes received on the current connection, from all other members.
const audioBytesIn = (page) => page.evaluate(async () => {
  const pc = window.__pcs.at(-1)
  if (!pc) return 0
  let bytes = 0
  ;(await pc.getStats()).forEach((s) => { if (s.type === 'inbound-rtp' && s.kind === 'audio') bytes += s.bytesReceived || 0 })
  return bytes
})

async function apiFetch(page, method, path, body) {
  return page.evaluate(async ({ method, path, body }) => {
    const res = await fetch(path, { method, credentials: 'same-origin', headers: body ? { 'Content-Type': 'application/json' } : {}, body: body ? JSON.stringify(body) : undefined })
    return { status: res.status, json: await res.json().catch(() => null) }
  }, { method, path, body })
}

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
  const invite = await apiFetch(first, 'POST', '/api/admin/invites', { max_uses: 1 })
  await late.goto('/')
  expect((await apiFetch(late, 'POST', '/api/auth/register', { username: USER, password: USER_PASSWORD, invite_code: invite.json.code })).status).toBe(201)
  await first.reload()
  await late.reload()

  const sidebarVoice = (page) => page.locator('[data-channel-type="voice"]', { hasText: VOICE_CHANNEL })
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
    expect((await filterState(page)).failed).toBe(false)
  }
  // The first microphone keeps processing after the other member starts its
  // own filter; counts reflect accepted PCM replies from the actual Worker.
  await expect.poll(async () => (await filterState(first)).processed, { timeout: 15_000 }).toBeGreaterThan(beforeLateJoin)

  await firstCtx.close()
  await lateCtx.close()
})
