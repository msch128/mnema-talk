import { test, expect } from './fixtures'
import type { BrowserContext, Page } from '@playwright/test'
import { apiFetch, awaitApiBudget, expectTalkLive, leaveTalks, responseString, responseUserId, trackPeerConnections } from './helpers'

// A Talk started in a direct message rings the other member (doc/talks.md,
// "Start a Talk from a direct message"): an incoming call card wherever they
// are, a phone on the conversation in the list, and a missed call when nobody
// answers. Two members in separate contexts; every UI string is German (the
// suite's locale). It needs calls (fake microphone), so it runs in CI only.

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'cool_guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
const CALLEE = `callee_${suffix}`
const CALLEE_PASSWORD = `pw-${suffix}-callee-x1`
const OPTIONS = { permissions: ['microphone' as const], viewport: { width: 1400, height: 900 } }

async function signIn(page: Page, username: string, password: string) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByTestId('login-heading')).toBeVisible()
  await dialog.getByLabel('Benutzername').fill(username)
  await dialog.getByLabel('Passwort').fill(password)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

interface World {
  owner: Page
  callee: Page
  /** A second window of the callee: another device. */
  calleeTab: Page
  contexts: BrowserContext[]
  dm: string
}

// The member and the conversation are made once and used by both specs: the
// account limits (ten sign-ups an hour, three new conversations a minute for
// the Owner) are shared with the whole suite.
const shared = { calleeId: '', dm: '' }

/** The Owner and a member of this spec's own, a conversation between them. There is no Talk lobby: a call works on a fresh community. */
async function world(browser: import('@playwright/test').Browser): Promise<World> {
  const ownerCtx = await browser.newContext(OPTIONS)
  const calleeCtx = await browser.newContext(OPTIONS)
  for (const ctx of [ownerCtx, calleeCtx]) await ctx.addInitScript(trackPeerConnections)
  const owner = await ownerCtx.newPage()
  const callee = await calleeCtx.newPage()
  await signIn(owner, ADMIN_USER, ADMIN_PASSWORD)
  if (await awaitApiBudget(owner)) await owner.reload()

  await callee.goto('/')
  if (!shared.calleeId) {
    const invite = await apiFetch(owner, 'POST', '/api/admin/invites', { max_uses: 1 })
    const registered = await apiFetch(callee, 'POST', '/api/auth/register', { username: CALLEE, password: CALLEE_PASSWORD, invite_code: responseString(invite, 'code') })
    expect(registered.status).toBe(201)
    shared.calleeId = responseUserId(await apiFetch(callee, 'GET', '/api/auth/me'))
    // The member starts the conversation: the starter's cap on unanswered messages is theirs, not the Owner's.
    const ownerId = responseUserId(await apiFetch(owner, 'GET', '/api/auth/me'))
    const dm = await apiFetch(callee, 'POST', '/api/dms', { user_ids: [ownerId] })
    expect([200, 201]).toContain(dm.status)
    shared.dm = responseString(dm, 'id')
  } else {
    expect((await apiFetch(callee, 'POST', '/api/auth/login', { username: CALLEE, password: CALLEE_PASSWORD })).status).toBe(200)
  }
  await callee.goto('/')

  // The callee's other device: the same session in a second window.
  const calleeTab = await calleeCtx.newPage()
  await calleeTab.goto('/')
  await owner.goto('/')
  return { owner, callee, calleeTab, contexts: [ownerCtx, calleeCtx], dm: shared.dm }
}

async function cleanUp(w: World) {
  for (const ctx of w.contexts) await ctx.close()
}

const dmRow = (page: Page, dm: string) => page.locator(`[data-dm="${dm}"]`)

async function startCall(w: World) {
  await dmRow(w.owner, w.dm).getByTestId('dm-open').click()
  await w.owner.getByTestId('dm-start-talk').click()
}

test('a call rings every window of the member and stops everywhere when one joins', async ({ browser }) => {
  test.setTimeout(240_000) // two calls and the grace time of the empty Talk
  const w = await world(browser)
  try {
    await startCall(w)
    await expectTalkLive(w.owner)

    // The callee sees who calls, wherever they are, in both windows and on the conversation's row.
    for (const page of [w.callee, w.calleeTab]) {
      const card = page.getByTestId('incoming-call')
      await expect(card).toBeVisible()
      await expect(card).toContainText(ADMIN_USER)
      await expect(card).toContainText('ruft dich an')
      await expect(dmRow(page, w.dm).getByTestId('dm-call')).toHaveText('Ruft dich an')
    }
    // The caller reads that they are calling and can cancel.
    await expect(w.owner.getByTestId('outgoing-call')).toContainText(`Ruft ${CALLEE} an`)

    // Dismissing closes the card in that window only; the call goes on.
    await w.calleeTab.getByTestId('incoming-call-dismiss').click()
    await expect(w.calleeTab.getByTestId('incoming-call')).toHaveCount(0)
    await expect(w.callee.getByTestId('incoming-call')).toBeVisible()

    // Joining here ends the ring on the other window and for the caller.
    await w.callee.getByTestId('incoming-call-join').click()
    await expectTalkLive(w.callee)
    await expect(w.callee.getByTestId('incoming-call')).toHaveCount(0)
    await expect(w.owner.getByTestId('outgoing-call')).toHaveCount(0)
    await expect(dmRow(w.calleeTab, w.dm).getByTestId('dm-call')).toHaveText('Im Talk')

    await leaveTalks([w.owner, w.callee])
    await expect(dmRow(w.calleeTab, w.dm).getByTestId('dm-call')).toHaveCount(0, { timeout: 15_000 })
  } finally {
    await cleanUp(w)
  }
})

test('a call the caller gives up leaves a missed call in the conversation', async ({ browser }) => {
  test.setTimeout(180_000)
  const w = await world(browser)
  try {
    await startCall(w)
    await expectTalkLive(w.owner)
    await expect(w.callee.getByTestId('incoming-call')).toBeVisible()

    await w.owner.getByTestId('outgoing-call-cancel').click()
    await expect(w.callee.getByTestId('incoming-call')).toHaveCount(0)
    await expect(w.calleeTab.getByTestId('incoming-call')).toHaveCount(0)

    // The record is in the conversation and the row says so, with a badge for what is new.
    await dmRow(w.callee, w.dm).getByTestId('dm-open').click()
    await expect(w.callee.getByTestId('call-missed')).toContainText(`${ADMIN_USER} hat dich angerufen`)
    await expect(w.callee.getByTestId('call-missed-back')).toBeVisible()
    await expect(dmRow(w.calleeTab, w.dm).getByTestId('dm-preview')).toHaveText('Hat angerufen', { timeout: 15_000 })

    // The caller reads it from their side.
    await dmRow(w.owner, w.dm).getByTestId('dm-open').click()
    await expect(w.owner.getByTestId('call-missed')).toContainText('Niemand ist beigetreten')
  } finally {
    await cleanUp(w)
  }
})
