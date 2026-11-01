import type { Page } from '@playwright/test'
import { apiFetch, responseString, suiteMember, trackPeerConnections } from './helpers'
import { test, expect } from './fixtures'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'cool_guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
const VOICE_CHANNEL = `Dtx ${suffix}`

// The browser is asked for discontinuous transmission on the microphone line
// (usedtx=1 in the SFU's offer; the media worker also drops the empty frames the
// encoder hands over, or sealing would send them), so a speaker who says nothing
// sends about 5 small packets a second instead of 50, and goes back to the full rate when speaking,
// while the listener's audio goes on without a break (comfort noise fills the
// gaps). Chromium's own fake microphone cannot show this: its beeps leave long
// decaying tails after the app's filters, which the encoder never takes for
// silence. So the page gets a microphone it controls: true silence, or a
// speech-like sound (a buzzing voice through two formants, a pitch that wobbles
// and syllables four times a second).
const controllableMicrophone = () => {
  localStorage.setItem('mnema_noise', 'browser')
  const real = navigator.mediaDevices.getUserMedia.bind(navigator.mediaDevices)
  navigator.mediaDevices.getUserMedia = async (constraints) => {
    if (!constraints?.audio) return real(constraints)
    const ctx = new AudioContext({ sampleRate: 48000 })
    void ctx.resume()
    const destination = ctx.createMediaStreamDestination()
    const master = ctx.createGain()
    master.gain.value = 0
    master.connect(destination)
    const voice = ctx.createOscillator()
    voice.type = 'sawtooth'
    voice.frequency.value = 130
    const vibrato = ctx.createOscillator()
    vibrato.frequency.value = 5
    const vibratoDepth = ctx.createGain()
    vibratoDepth.gain.value = 25
    vibrato.connect(vibratoDepth).connect(voice.frequency)
    const syllables = ctx.createGain()
    syllables.gain.value = 0.5
    const syllableLfo = ctx.createOscillator()
    syllableLfo.frequency.value = 4
    const syllableDepth = ctx.createGain()
    syllableDepth.gain.value = 0.45
    syllableLfo.connect(syllableDepth).connect(syllables.gain)
    for (const hz of [700, 1500]) {
      const formant = ctx.createBiquadFilter()
      formant.type = 'bandpass'
      formant.frequency.value = hz
      formant.Q.value = 3
      voice.connect(formant).connect(syllables)
    }
    syllables.connect(master)
    for (const node of [voice, vibrato, syllableLfo]) node.start()
    ;(window as unknown as { __speak: (on: boolean) => void }).__speak = (on) => { master.gain.value = on ? 1.2 : 0 }
    return destination.stream
  }
}

const speak = (page: Page, on: boolean) => page.evaluate((v) => (window as unknown as { __speak: (on: boolean) => void }).__speak(v), on)

/** Audio the page received from the other members: packets and samples played out. */
const audioIn = (page: Page) => page.evaluate(async () => {
  const total = { packets: 0, samples: 0 }
  for (const pc of window.__pcs) {
    for (const stat of (await pc.getStats()).values()) {
      if (stat.type === 'inbound-rtp' && stat.kind === 'audio') {
        total.packets += stat.packetsReceived ?? 0
        total.samples += stat.totalSamplesReceived ?? 0
      }
    }
  }
  return total
})

async function window_(page: Page, seconds: number) {
  const before = await audioIn(page)
  await page.waitForTimeout(seconds * 1000)
  const after = await audioIn(page)
  return { perSecond: (after.packets - before.packets) / seconds, samples: after.samples - before.samples }
}

async function signIn(page: Page, username: string, password: string) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('Benutzername').fill(username)
  await dialog.getByLabel('Passwort').fill(password)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

test('a silent speaker sends a few small packets, a speaking one the full rate, and the listener plays continuous audio', async ({ browser, browserName }) => {
  test.skip(browserName !== 'chromium', 'The packet rates are those of Chromium\'s Opus encoder')
  test.setTimeout(150_000)
  const speakerCtx = await browser.newContext({ permissions: ['microphone'], viewport: { width: 1400, height: 900 } })
  const listenerCtx = await browser.newContext({ permissions: ['microphone'], viewport: { width: 1400, height: 900 } })
  await speakerCtx.addInitScript(trackPeerConnections)
  await speakerCtx.addInitScript(controllableMicrophone)
  await listenerCtx.addInitScript(trackPeerConnections)
  const speaker = await speakerCtx.newPage()
  const listener = await listenerCtx.newPage()
  let channelId = ''
  try {
    await signIn(speaker, ADMIN_USER, ADMIN_PASSWORD)
    const created = await apiFetch(speaker, 'POST', '/api/admin/channels', { name: VOICE_CHANNEL, type: 'voice' })
    expect(created.status).toBe(201)
    channelId = responseString(created, 'id')
    await listener.goto('/')
    if (suiteMember.registered) {
      expect((await apiFetch(listener, 'POST', '/api/auth/login', { username: suiteMember.username, password: suiteMember.password })).status).toBe(200)
    } else {
      const invite = await apiFetch(speaker, 'POST', '/api/admin/invites', { max_uses: 1 })
      expect((await apiFetch(listener, 'POST', '/api/auth/register', { username: suiteMember.username, password: suiteMember.password, invite_code: responseString(invite, 'code') })).status).toBe(201)
      suiteMember.registered = true
    }
    await listener.reload()

    for (const page of [speaker, listener]) {
      await page.locator('[data-channel-type="voice"]', { hasText: VOICE_CHANNEL }).click()
      await expect(page.getByRole('button', { name: 'Verlassen' }).first()).toBeVisible()
    }
    await expect.poll(() => speaker.evaluate(() => window.__pcs.at(-1)?.connectionState), { timeout: 20_000 }).toBe('connected')

    // The offer the speaker's browser applied asks for DTX on the microphone line, and only there.
    const offer = await speaker.evaluate(() => window.__pcs.at(-1)?.remoteDescription?.sdp ?? '')
    expect(offer).toMatch(/a=fmtp:\d+ [^\r\n]*usedtx=1/)
    expect(offer.match(/usedtx=1/g)).toHaveLength(1)

    // Nothing is said: the packets nearly stop (about 5 a second), and what is played out does not.
    await speak(speaker, false)
    await listener.waitForTimeout(3000)
    const quiet = await window_(listener, 6)
    expect(quiet.perSecond).toBeLessThan(15)
    expect(quiet.samples).toBeGreaterThan(6 * 48000 * 0.9)

    // Speech: the full rate again.
    await speak(speaker, true)
    await listener.waitForTimeout(2000)
    const talking = await window_(listener, 4)
    expect(talking.perSecond).toBeGreaterThan(35)
    expect(talking.samples).toBeGreaterThan(4 * 48000 * 0.9)

    // And back to quiet.
    await speak(speaker, false)
    await listener.waitForTimeout(3000)
    expect((await window_(listener, 4)).perSecond).toBeLessThan(15)
  } finally {
    for (const page of [listener, speaker]) await page.getByRole('button', { name: 'Verlassen' }).first().click({ timeout: 3000 }).catch(() => undefined)
    if (channelId) await apiFetch(speaker, 'DELETE', `/api/admin/channels/${channelId}`).catch(() => undefined)
    await listenerCtx.close()
    await speakerCtx.close()
  }
})
