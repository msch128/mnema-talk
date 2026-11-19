import { randomBytes } from 'node:crypto'
import { expect, test } from '@playwright/test'
import type { BrowserContext, Page, Locator } from '@playwright/test'

// The invite-only sign-up allows ten registrations an hour from one address and
// the whole suite shares them, so specs that need a second member share one.
export const suiteMember = {
  username: `member_${Date.now().toString(36)}`,
  password: `pw-${Date.now().toString(36)}-${randomBytes(8).toString('hex')}`,
  registered: false,
}
import { decodeChatChannelHierarchy } from '../../web/src/types/rest'
import { decodeUser } from '../../web/src/types/domain'
import type { ChatChannelHierarchy } from '../../web/src/types/rest'

export interface ApiResponse<T = unknown> { status: number; json: T }

export async function bounds(locator: Locator): Promise<{ x: number; y: number; width: number; height: number }> {
  const box = await locator.boundingBox()
  if (!box) throw new Error('Expected visible element bounds')
  return box
}

// Preserve browser cookies and Origin checks; validate the returned wire data
// in the harness before a test treats it as a typed application object.
export function apiFetch(page: Page, method: 'GET', path: '/api/channels', body?: unknown): Promise<ApiResponse<ChatChannelHierarchy>>
export function apiFetch(page: Page, method: string, path: string, body?: unknown): Promise<ApiResponse>
export async function apiFetch(page: Page, method: string, path: string, body?: unknown): Promise<ApiResponse> {
  // Every spec signs in as the same administrator, so they share one request
  // budget (600 a minute); a spec that starts inside an exhausted window waits
  // it out instead of failing.
  let result = await fetchOnce(page, method, path, body)
  for (let attempt = 0; attempt < 4 && result.status === 429; attempt++) {
    // The window is fixed: ask again until the budget is back.
    await expectBudgetBack(page)
    result = await fetchOnce(page, method, path, body)
  }
  if (method === 'GET' && path === '/api/channels') {
    if (result.status !== 200) throw new Error(`Expected channel hierarchy, received HTTP ${result.status}`)
    return { ...result, json: decodeChatChannelHierarchy(result.json) }
  }
  return result
}

function fetchOnce(page: Page, method: string, path: string, body: unknown): Promise<ApiResponse> {
  return page.evaluate(async ({ method, path, body }) => {
    const response = await fetch(path, {
      method, credentials: 'same-origin',
      ...(body === undefined ? {} : { headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) }),
    })
    const json: unknown = await response.json().catch(() => null)
    return { status: response.status, json }
  }, { method, path, body })
}

// The API allows 600 requests a minute per account and every spec signs in as
// the same administrator from one address, so a spec that follows a busy one
// can start inside an exhausted window. Waits out the Retry-After of such a
// 429 and reports whether the page was throttled, so the caller can reload it.
export async function awaitApiBudget(page: Page): Promise<boolean> {
  let throttled = false
  for (let attempt = 0; attempt < 3; attempt++) {
    if (await meStatus(page) !== 429) break
    throttled = true
    await expectBudgetBack(page)
  }
  return throttled
}

async function meStatus(page: Page): Promise<number> {
  return page.evaluate(async () => (await fetch('/api/auth/me', { credentials: 'same-origin' })).status)
}

// The request budget is a fixed one-minute window, so it is back within 65 s.
async function expectBudgetBack(page: Page) {
  await expect.poll(() => meStatus(page), { timeout: 70_000, intervals: [2_000], message: 'the API request budget did not come back' }).not.toBe(429)
}

/**
 * Resolves once every finite animation and transition of the page has ended (a
 * dialog fading in, a staggered list), so what follows reads the settled layout
 * and colours. A spinner that turns for ever does not count.
 */
export async function awaitSettled(page: Page) {
  await page.evaluate(async () => {
    const frame = () => new Promise<void>(resolve => requestAnimationFrame(() => resolve()))
    await frame()
    for (let round = 0; round < 10; round++) {
      const running = document.getAnimations().filter(a => a.playState !== 'finished' && a.effect?.getComputedTiming().iterations !== Infinity)
      if (running.length === 0) return
      await Promise.allSettled(running.map(a => a.finished))
      await frame()
    }
  })
}

/** Resolves once the page has stopped scrolling (a smooth scroll has landed): its position held for 5 frames in a row. */
export async function awaitScrollEnd(page: Page) {
  await page.evaluate(() => new Promise<void>((resolve, reject) => {
    let last = ''
    let still = 0
    const deadline = performance.now() + 5_000
    const step = () => {
      const now = Math.round(window.scrollY * 2) + ':' + Math.round(document.scrollingElement?.scrollTop ?? 0)
      still = last === now ? still + 1 : 0
      last = now
      if (still >= 5) return resolve()
      if (performance.now() > deadline) return reject(new Error('the page kept scrolling'))
      requestAnimationFrame(step)
    }
    requestAnimationFrame(step)
  }))
}

/**
 * Lets a stretch of real time pass in the page, for a measurement over a time
 * span (bytes in ten seconds, packets a second). Where a state can be awaited,
 * use a web-first assertion instead; this is only for rates and for proving
 * that nothing happens.
 */
export async function observeFor(page: Page, ms: number) {
  await page.evaluate((span) => new Promise<void>(resolve => setTimeout(resolve, span)), ms)
}

export function responseString(response: ApiResponse, field: string): string {
  const value = response.json
  if (value === null || typeof value !== 'object' || Array.isArray(value)) throw new Error('Expected an object response')
  const property: unknown = Object.getOwnPropertyDescriptor(value, field)?.value
  if (typeof property !== 'string' || property.length === 0) throw new Error(`Expected a string response field: ${field}`)
  return property
}

export function responseUserId(response: ApiResponse): string {
  // /api/auth/me returns User directly; login/register return UserEnvelope.
  return decodeUser(response.json).id
}

/**
 * Init script of every browser context (fixtures.ts adds it when the context
 * is created, so no spec can forget it): it records the page's peer
 * connections in window.__pcs and window.__pcTimings, and it turns the AI
 * noise filter off. The default (DeepFilterNet3) loads an 8 MB model and runs
 * it in WebAssembly for each member, and on the two cores of a hosted macOS
 * runner two members starve each other, so joining a call took 20 s instead of
 * 3 and the specs' 20 s waits for the connection, for decoded audio or for the
 * first camera or screen frames ran out. The specs are about the call, not the
 * filter; a spec that checks the filter (late-join, perf-idle, audio-stall)
 * turns it back on with an init script of its own, which runs after this one.
 * The script runs on every page load, so it only sets the filter while the
 * context has none stored yet: a choice a test makes later (in the settings
 * or by its own init script) survives a reload, and a spec that checks that
 * the setting persisted needs no way around this script.
 * A spec must not wrap RTCPeerConnection to record it again: every connection
 * would be in window.__pcs twice.
 */
export function trackPeerConnections(): void {
  // A frame without storage (about:blank, a sandboxed frame) throws here.
  try {
    if (localStorage.getItem('mnema_noise') === null) localStorage.setItem('mnema_noise', 'off')
  } catch { /* no storage in this frame */ }
  const Native = window.RTCPeerConnection
  window.__pcs = []
  window.__pcTimings = []
  window.RTCPeerConnection = class extends Native {
    constructor(...args: ConstructorParameters<typeof RTCPeerConnection>) {
      super(...args)
      window.__pcs.push(this)
      // Milliseconds from creation, per milestone: told apart because a Firefox connection reaches
      // "connected" within ms while its ICE gathering only completes after ~11 s (seen in CI server logs).
      const t0 = performance.now()
      const timing: Record<string, number> = {}
      window.__pcTimings.push(timing)
      const mark = (name: string) => { timing[name] ??= Math.round(performance.now() - t0) }
      this.addEventListener('iceconnectionstatechange', () => { if (this.iceConnectionState === 'connected') mark('iceConnected') })
      this.addEventListener('connectionstatechange', () => { if (this.connectionState === 'connected') mark('connected') })
      this.addEventListener('icegatheringstatechange', () => { if (this.iceGatheringState === 'complete') mark('gatheringComplete') })
    }
  }
}

/**
 * Audio the browser decoded from the other members on the current connection.
 * Packets it could not open never reach the decoder, which then only makes up
 * samples for them (counted as concealed), so this stays put for them; the byte
 * count would not. It does not depend on how loud an engine's fake microphone
 * is or on what the noise filter makes of it.
 */
export function audioDecodedIn(page: Page): Promise<number> {
  return page.evaluate(async () => {
    const pc = window.__pcs.at(-1)
    let samples = 0
    if (pc) (await pc.getStats()).forEach((s) => { if (s.type === 'inbound-rtp' && s.kind === 'audio') samples += (s.totalSamplesReceived || 0) - (s.concealedSamples || 0) })
    return samples
  })
}

/** The page's own call is up: after a reload it rejoins by itself, and leaving before that leaves nothing for the server to end. */
export async function expectTalkLive(page: Page) {
  await expectCallConnected(page, 20_000)
}

/**
 * Waits for the page's current connection to be 'connected'. When it is not in
 * time, the failure says how far it got (ICE and DTLS state, each candidate
 * pair's checks, candidate types, never addresses), so a call stuck at
 * 'connecting' is told apart from one that never gathered or never got an
 * answer. The server logs the same for its side as "sfu peer still connecting".
 */
export async function expectCallConnected(page: Page, timeout: number) {
  const state = () => page.evaluate(() => window.__pcs.at(-1)?.connectionState)
  try {
    await expect.poll(state, { timeout }).toBe('connected')
    // Setup times per browser in the CI log, so a slow join shows up as a number before it shows up as a timeout.
    const timing = await page.evaluate(() => window.__pcTimings.at(-1)).catch(() => undefined)
    console.log(`[call timing] ${test.info().project.name} ${test.info().title}: ${JSON.stringify(timing)}`)
  } catch (err) {
    const summary = await callSummary(page).catch((e: unknown) => `unavailable: ${String(e)}`)
    throw new Error(`call not connected within ${timeout} ms: ${summary}\n${String(err)}`, { cause: err })
  }
}

function callSummary(page: Page): Promise<string> {
  return page.evaluate(async () => {
    const pc = window.__pcs.at(-1)
    if (!pc) return 'no RTCPeerConnection'
    const kinds = new Map<string, string>()
    const pairs: string[] = []
    const transports: string[] = []
    const report = await pc.getStats()
    report.forEach((s) => {
      if (s.type === 'local-candidate' || s.type === 'remote-candidate') kinds.set(s.id, `${s.protocol}/${s.candidateType}`)
    })
    report.forEach((s) => {
      if (s.type === 'candidate-pair') {
        pairs.push(`${kinds.get(s.localCandidateId)}>${kinds.get(s.remoteCandidateId)} ${s.state} req=${s.requestsSent}/${s.requestsReceived} resp=${s.responsesReceived}/${s.responsesSent}${s.nominated ? ' nominated' : ''}`)
      } else if (s.type === 'transport') {
        transports.push(`dtls=${s.dtlsState} ice=${s.iceState ?? '?'}`)
      }
    })
    return `connection=${pc.connectionState} ice=${pc.iceConnectionState} gathering=${pc.iceGatheringState} signaling=${pc.signalingState}`
      + ` remoteDescription=${pc.remoteDescription ? 'set' : 'none'} pairs=[${pairs.join('; ')}] transport=[${transports.join('; ')}]`
  })
}

/**
 * Leaves the Talk with the Leave button on every page, so the server drops the
 * participants at once. Closing the context instead drops the socket and the
 * server keeps the caller listed for the 30 s voice grace period, which shifts
 * the sidebar rows of whatever spec runs next. The last page still has to see
 * the Talk empty well inside that period.
 */
export async function leaveTalks(pages: Page[]) {
  for (const page of pages) {
    await page.getByRole('button', { name: 'Verlassen' }).first().click()
    await expect(page.getByRole('button', { name: 'Verlassen' })).toHaveCount(0)
  }
  const last = pages.at(-1)
  if (last) await expect(last.locator('nav [data-voice-user]')).toHaveCount(0, { timeout: 15_000 })
}

/**
 * Leaves every Talk the given contexts are in, then closes them. For the
 * afterEach of a call spec: a failed attempt must not leave its members in the
 * Talk, because the retry signs in as the same members and its join would meet
 * the "take over the Talk here?" dialog (the old session stays listed for the
 * 30 s voice grace period) instead of showing whether the call works.
 */
export async function leaveAndClose(contexts: BrowserContext[]) {
  for (const ctx of contexts.splice(0)) {
    for (const page of ctx.pages()) {
      const leave = page.getByRole('button', { name: 'Verlassen' }).first()
      if (await leave.isVisible().catch(() => false)) await leave.click({ timeout: 2_000 }).catch(() => undefined)
    }
    await ctx.close().catch(() => undefined)
  }
}
