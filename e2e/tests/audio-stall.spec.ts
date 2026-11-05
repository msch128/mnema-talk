import type { Page } from '@playwright/test'
import { apiFetch, responseString, suiteMember, trackPeerConnections, leaveTalks, expectTalkLive } from './helpers'
import { test, expect } from './fixtures'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'cool_guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)

// A browser whose audio engine never starts (Firefox on a machine without an
// audio output device, or any browser holding audio back until a gesture) keeps
// its AudioContext suspended for good, and everything routed through the context
// goes silent. The page is made to see that state in every engine; where the
// engine is really stalled (Firefox without an audio device in CI) this changes
// nothing.
const stallAudioEngine = () => {
  localStorage.setItem('mnema_noise', 'ai')
  // The fake microphone is a steady tone that Opus DTX would take for silence; this spec listens for it.
  localStorage.setItem('mnema_dtx', 'false')
  Object.defineProperty(AudioContext.prototype, 'state', { configurable: true, get: () => 'suspended' })
  AudioContext.prototype.resume = () => new Promise<void>(() => {})
}

const outboundBytes = (page: Page) => page.evaluate(async () => {
  const pc = window.__pcs.at(-1)
  let bytes = 0
  if (pc) (await pc.getStats()).forEach((s) => { if (s.type === 'outbound-rtp' && s.kind === 'audio') bytes += s.bytesSent || 0 })
  return bytes
})
const inboundBytes = (page: Page) => page.evaluate(async () => {
  const pc = window.__pcs.at(-1)
  let bytes = 0
  if (pc) (await pc.getStats()).forEach((s) => { if (s.type === 'inbound-rtp' && s.kind === 'audio') bytes += s.bytesReceived || 0 })
  return bytes
})
// Where the engine reports it (Chromium), how much energy the other member's audio carries; a silenced track sends frames too, so bytes alone prove nothing.
const inboundEnergy = (page: Page) => page.evaluate(async () => {
  const pc = window.__pcs.at(-1)
  let energy: number | undefined
  if (pc) (await pc.getStats()).forEach((s) => { if (s.type === 'inbound-rtp' && s.kind === 'audio' && typeof s.totalAudioEnergy === 'number') energy = (energy ?? 0) + s.totalAudioEnergy })
  return energy
})
// What a failure needs to be explained afterwards: both ends' audio stats, the sending track and the connection state, as plain data.
const audioDiagnostics = (page: Page) => page.evaluate(async () => {
  const pc = window.__pcs.at(-1)
  if (!pc) return { pc: null }
  const stats: Record<string, unknown>[] = []
  ;(await pc.getStats()).forEach((s) => {
    if ((s.type === 'outbound-rtp' || s.type === 'inbound-rtp') && s.kind === 'audio') {
      const { type, bytesSent, packetsSent, bytesReceived, packetsReceived, packetsLost, jitter, totalAudioEnergy, totalSamplesDuration, concealedSamples, audioLevel, active, timestamp } = s
      stats.push({ type, bytesSent, packetsSent, bytesReceived, packetsReceived, packetsLost, jitter, totalAudioEnergy, totalSamplesDuration, concealedSamples, audioLevel, active, timestamp })
    }
  })
  const track = pc.getSenders().find(s => s.track?.kind === 'audio')?.track
  return {
    connection: pc.connectionState, ice: pc.iceConnectionState, signaling: pc.signalingState,
    track: track ? { enabled: track.enabled, muted: track.muted, readyState: track.readyState, label: track.label } : null,
    pcCount: window.__pcs.length, stats
  }
})
const sendingTrackEnabled = (page: Page) => page.evaluate(() => window.__pcs.at(-1)?.getSenders().find(s => s.track?.kind === 'audio')?.track?.enabled ?? false)

async function signIn(page: Page, username: string, password: string) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('Benutzername').fill(username)
  await dialog.getByLabel('Passwort').fill(password)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

test('a member whose audio engine never starts is told and is still heard', async ({ browser, browserName }, testInfo) => {
  const stalledCtx = await browser.newContext({ permissions: ['microphone', 'camera'] })
  const otherCtx = await browser.newContext({ permissions: ['microphone', 'camera'] })
  await stalledCtx.addInitScript(trackPeerConnections)
  await stalledCtx.addInitScript(stallAudioEngine)
  await otherCtx.addInitScript(trackPeerConnections)
  const stalled = await stalledCtx.newPage()
  const other = await otherCtx.newPage()

  await signIn(other, ADMIN_USER, ADMIN_PASSWORD)
  const channel = `Stall ${suffix}`
  expect((await apiFetch(other, 'POST', '/api/admin/channels', { name: channel, type: 'voice' })).status).toBe(201)
  if (suiteMember.registered) {
    await signIn(stalled, suiteMember.username, suiteMember.password)
  } else {
    const invite = await apiFetch(other, 'POST', '/api/admin/invites', { max_uses: 1 })
    await stalled.goto('/')
    expect((await apiFetch(stalled, 'POST', '/api/auth/register', { username: suiteMember.username, password: suiteMember.password, invite_code: responseString(invite, 'code') })).status).toBe(201)
    suiteMember.registered = true
  }
  await other.reload()
  await stalled.reload()

  try {
    const sidebarVoice = (page: Page) => page.locator('[data-channel-type="voice"]', { hasText: channel })
    await sidebarVoice(other).click()
    await expectTalkLive(other)
    await sidebarVoice(stalled).click()
    await expectTalkLive(stalled)

    // The member is told once, calmly, within the guard's few seconds.
    await expect(stalled.getByText(/Die Audioverarbeitung hat in diesem Browser nicht gestartet/)).toHaveCount(1, { timeout: 15_000 })

    // And the microphone goes out as it is: the voice gate cannot read a meter that never runs, so it stays open (a closed track sends silence, in as many bytes).
    await expect.poll(() => sendingTrackEnabled(stalled), { timeout: 15_000 }).toBe(true)
    // The tone must arrive as transport: Opus frames of a tone are several times the size of frames of silence, in every engine.
    await expect.poll(async () => {
      const before = { out: await outboundBytes(stalled), inn: await inboundBytes(other) }
      await stalled.waitForTimeout(3000)
      const delta = { out: (await outboundBytes(stalled)) - before.out, inn: (await inboundBytes(other)) - before.inn }
      return Math.min(delta.out, delta.inn)
    }, { timeout: 30_000, intervals: [500], message: 'the stalled member\'s microphone did not reach the other member as tone-sized audio (see the audio-diagnostics attachment)' }).toBeGreaterThan(6000)
    // Where the engine measures energy at playout, it must agree. Firefox counts it only for audio it is actually playing, and its
    // audio engine can be as stalled in CI as the stalled member's: there it is reported in the diagnostics, not asserted.
    if (browserName !== 'firefox' && await inboundEnergy(other) !== undefined) await expect.poll(async () => (await inboundEnergy(other)) ?? 0, { timeout: 15_000 }).toBeGreaterThan(0.001)
  } catch (err) {
    await testInfo.attach('audio-diagnostics', {
      body: JSON.stringify({ stalled: await audioDiagnostics(stalled).catch(String), other: await audioDiagnostics(other).catch(String) }, null, 2),
      contentType: 'application/json'
    })
    throw err
  }

  await leaveTalks([stalled, other])
  await stalledCtx.close()
  await otherCtx.close()
})
