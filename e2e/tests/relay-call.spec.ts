import type { Page } from '@playwright/test'
import { apiFetch, awaitApiBudget, responseString, suiteMember, leaveTalks, expectTalkLive, trackPeerConnections, audioDecodedIn } from './helpers'
import { test, expect } from './fixtures'

// A call whose browsers may only use the TURN relay built into the server
// (iceTransportPolicy 'relay', as behind a firewall that drops direct UDP):
// audio must flow both ways through the relay. e2e/run.sh starts the server
// with WEBRTC_BUILTIN_TURN=on. Needs a call (fake microphone), so it runs in CI
// only.

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'Herzog'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
const TALK = `Relay ${suffix}`

// Every peer connection of the page is created with iceTransportPolicy
// 'relay', so it gathers and uses relay candidates only.
const forceRelay = () => {
  const Native = window.RTCPeerConnection
  window.RTCPeerConnection = class extends Native {
    constructor(...args: ConstructorParameters<typeof RTCPeerConnection>) {
      super({ ...(args[0] ?? {}), iceTransportPolicy: 'relay' })
    }
  }
}

async function signIn(page: Page, username: string, password: string) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('Benutzername').fill(username)
  await dialog.getByLabel('Passwort').fill(password)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

// The candidate type this page's connection is using locally, from its selected pair.
const selectedLocalCandidateType = (page: Page) => page.evaluate(async () => {
  const pc = window.__pcs.at(-1)
  if (!pc) return ''
  const stats = await pc.getStats()
  let pairLocal = ''
  stats.forEach((s) => {
    if (s.type === 'candidate-pair' && (s.nominated || s.selected) && s.state === 'succeeded') pairLocal = s.localCandidateId
  })
  let type = ''
  stats.forEach((s) => { if (s.type === 'local-candidate' && s.id === pairLocal) type = s.candidateType })
  return type
})

test('a call over the built-in TURN relay carries audio both ways', async ({ browser, browserName }) => {
  test.skip(browserName === 'webkit', 'Calls need RTCRtpScriptTransform, which Playwright WebKit on Linux lacks; the relay path is browser independent')
  test.setTimeout(180_000)
  const ctxOptions = { permissions: ['microphone' as const], viewport: { width: 1400, height: 900 } }
  const aCtx = await browser.newContext(ctxOptions)
  const bCtx = await browser.newContext(ctxOptions)
  for (const ctx of [aCtx, bCtx]) {
    await ctx.addInitScript(trackPeerConnections)
    await ctx.addInitScript(forceRelay)
  }
  const a = await aCtx.newPage()
  const b = await bCtx.newPage()

  await signIn(a, ADMIN_USER, ADMIN_PASSWORD)
  if (await awaitApiBudget(a)) await a.reload()

  // The server offers its own relay to a member who may join voice.
  const config = await apiFetch(a, 'GET', '/api/webrtc/config')
  expect(config.status).toBe(200)
  const servers = (config.json as { ice_servers: Array<{ urls: string[]; username?: string; credential?: string }> }).ice_servers
  const turn = servers.find((s) => s.urls.some((u) => u.startsWith('turn:')))
  expect(turn, 'the built-in relay is offered').toBeDefined()
  expect(turn?.urls.some((u) => u.endsWith('?transport=udp'))).toBe(true)
  expect(turn?.urls.some((u) => u.endsWith('?transport=tcp'))).toBe(true)
  expect(turn?.username && turn.credential).toBeTruthy()

  const created = await apiFetch(a, 'POST', '/api/admin/channels', { name: TALK, type: 'voice' })
  expect(created.status).toBe(201)
  const talkId = responseString(created, 'id')

  await b.goto('/')
  if (!suiteMember.registered) {
    const invite = await apiFetch(a, 'POST', '/api/admin/invites', { max_uses: 1 })
    expect((await apiFetch(b, 'POST', '/api/auth/register', { username: suiteMember.username, password: suiteMember.password, invite_code: responseString(invite, 'code') })).status).toBe(201)
    suiteMember.registered = true
  } else {
    expect((await apiFetch(b, 'POST', '/api/auth/login', { username: suiteMember.username, password: suiteMember.password })).status).toBe(200)
  }
  await b.reload()

  const row = (page: Page) => page.locator('[data-channel-type="voice"]', { hasText: TALK })
  await row(a).click()
  await expectTalkLive(a)
  await row(b).click()
  await expectTalkLive(b)

  // Audio decoded from the other member, on both sides, over relay candidates.
  for (const page of [a, b]) {
    await expect.poll(() => audioDecodedIn(page), { timeout: 30_000 }).toBeGreaterThan(24_000)
    expect(await selectedLocalCandidateType(page)).toBe('relay')
  }

  await leaveTalks([a, b])
  await apiFetch(a, 'DELETE', `/api/admin/channels/${talkId}`)
  await aCtx.close()
  await bCtx.close()
})
