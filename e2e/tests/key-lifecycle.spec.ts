import type { Browser, Page } from '@playwright/test'
import { apiFetch, responseString, responseUserId, suiteMember, trackPeerConnections, leaveTalks, audioDecodedIn } from './helpers'
import { test, expect } from './fixtures'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'Cool_Guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)

// The e2e build (MNEMA_E2E=1) lets a page shorten the media key lifetimes; a
// release build ignores this value. See web/src/lib/media/lifetime.ts.
interface Timing { rotateMs: number, rotateJitterMs: number, absentMs: number, heartbeatMs: number, maintenanceMs: number }
const NEVER = 3_600_000
const live: Timing = { rotateMs: NEVER, rotateJitterMs: 0, absentMs: NEVER, heartbeatMs: 1000, maintenanceMs: 250 }

const setTiming = (page: Page, timing: Timing) => page.addInitScript((value) => {
  (window as unknown as { __MNEMA_MEDIA_TIMING__: unknown }).__MNEMA_MEDIA_TIMING__ = value
}, timing)

async function signIn(page: Page, username: string, password: string) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('Benutzername').fill(username)
  await dialog.getByLabel('Passwort').fill(password)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}


// The safety code names the keys of the current epoch, so it changes with
// every new epoch. Null while the member holds no group.
async function safetyCode(page: Page): Promise<string | null> {
  await page.getByTestId('voice-panel-ping').click()
  const text = await page.getByTestId('media-safety-code').textContent({ timeout: 1500 }).catch(() => null)
  await page.keyboard.press('Escape')
  return text && /^\d{5} \d{5} \d{5} \d{5}$/.test(text) ? text : null
}

// The member with the lowest id commits (removes, rotates), so the roles follow
// the ids that this run's accounts happened to get.
async function setup(browser: Browser, committerTiming: Timing, otherTiming: Timing, channel: string) {
  const contexts = await Promise.all([0, 1].map(() => browser.newContext({ permissions: ['microphone', 'camera'] })))
  const [adminCtx, memberCtx] = contexts as [typeof contexts[0], typeof contexts[0]]
  for (const ctx of contexts) await ctx.addInitScript(trackPeerConnections)
  const admin = await adminCtx.newPage()
  const member = await memberCtx.newPage()
  await signIn(admin, ADMIN_USER, ADMIN_PASSWORD)
  expect((await apiFetch(admin, 'POST', '/api/admin/channels', { name: channel, type: 'voice' })).status).toBe(201)
  if (suiteMember.registered) {
    await signIn(member, suiteMember.username, suiteMember.password)
  } else {
    await member.goto('/')
    const invite = await apiFetch(admin, 'POST', '/api/admin/invites', { max_uses: 1 })
    expect((await apiFetch(member, 'POST', '/api/auth/register', { username: suiteMember.username, password: suiteMember.password, invite_code: responseString(invite, 'code') })).status).toBe(201)
    suiteMember.registered = true
  }
  const adminId = responseUserId(await apiFetch(admin, 'GET', '/api/auth/me'))
  const memberId = responseUserId(await apiFetch(member, 'GET', '/api/auth/me'))
  const [committer, other] = adminId < memberId ? [admin, member] : [member, admin]
  await setTiming(committer, committerTiming)
  await setTiming(other, otherTiming)
  await admin.reload()
  await member.reload()
  const sidebarVoice = (page: Page) => page.locator('[data-channel-type="voice"]', { hasText: channel })
  await sidebarVoice(admin).click()
  await sidebarVoice(member).click()
  for (const page of [admin, member]) {
    await expect.poll(() => page.evaluate(() => window.__pcs.at(-1)?.connectionState), { timeout: 20_000 }).toBe('connected')
  }
  return { adminCtx, memberCtx, committer, other, pages: [committer, other] as Page[] }
}

const hearEachOther = async (pages: Page[]) => {
  for (const page of pages) {
    const before = await audioDecodedIn(page)
    await expect.poll(() => audioDecodedIn(page), { timeout: 20_000 }).toBeGreaterThan(before)
  }
}

async function sameCode(pages: Page[], not: string | null = null) {
  let agreed: string | null = null
  await expect.poll(async () => {
    const codes = [await safetyCode(pages[0] as Page), await safetyCode(pages[1] as Page)]
    const [one, two] = codes
    agreed = one && one === two && one !== not ? one : null
    return agreed
  }, { timeout: 30_000 }).not.toBeNull()
  return agreed as unknown as string
}

test.describe.configure({ mode: 'serial' })

test('the media key rotates on its own and audio keeps flowing across it', async ({ browser }) => {
  const timing = { ...live, rotateMs: 7000 }
  const { adminCtx, memberCtx, committer, pages } = await setup(browser, timing, timing, `Rotate ${suffix}`)
  await hearEachOther(pages)
  const connections = await committer.evaluate(() => window.__pcs.length)
  // Over several rotations both members agree on each epoch's code and hear each other.
  let code = await sameCode(pages)
  for (let i = 0; i < 3; i++) {
    code = await sameCode(pages, code)
    await hearEachOther(pages)
  }
  // No reconnect was needed for any of it.
  expect(await committer.evaluate(() => window.__pcs.length)).toBe(connections)
  await leaveTalks(pages)
  await adminCtx.close()
  await memberCtx.close()
})

test('a member that showed no sign of life is rekeyed out and is let back in when it returns', async ({ browser }) => {
  // The other member never sends a heartbeat (as a frozen tab would not).
  const { adminCtx, memberCtx, committer, other, pages } = await setup(
    browser,
    { ...live, absentMs: 5000 },
    { ...live, heartbeatMs: NEVER },
    `Absent ${suffix}`,
  )
  await hearEachOther(pages)
  const before = await sameCode(pages)
  // The committer removes the silent one: its group changes, the other holds none.
  await expect.poll(() => safetyCode(committer), { timeout: 30_000 }).not.toBe(before)
  // The silent member returns (reloads, now alive) and is let in with fresh keys.
  await setTiming(other, { ...live, absentMs: 5000 })
  await other.reload()
  await expect.poll(() => other.evaluate(() => window.__pcs.at(-1)?.connectionState), { timeout: 30_000 }).toBe('connected')
  const back = await sameCode(pages, before)
  await hearEachOther(pages)
  // It now beats, so it stays: the code holds well past the absence limit.
  await committer.waitForTimeout(9000)
  expect(await sameCode(pages)).toBe(back)
  await hearEachOther(pages)
  await leaveTalks(pages)
  await adminCtx.close()
  await memberCtx.close()
})
