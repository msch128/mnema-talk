import { test, expect } from './fixtures'
import type { Page } from '@playwright/test'
import { apiFetch, responseString, responseUserId } from './helpers'

// The list of direct messages (1.2): folding it, closing a conversation and
// seeing it return with the next message, favourites, and muting all of them.
// No media is involved. The Owner and the suite member sit in separate
// browser contexts; every UI string is German, so the checks use test ids.

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'cool_guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
const VIEWPORT = { width: 1400, height: 900 }

async function signIn(page: Page, username: string, password: string) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByTestId('login-heading')).toBeVisible()
  await dialog.getByLabel('Benutzername').fill(username)
  await dialog.getByLabel('Passwort').fill(password)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

/**
 * A member of this spec's own, so the suite member's conversations (which dm.spec.ts counts unread
 * messages in) are left alone. Registered by the first test, signed in by the second.
 */
const polish = { username: `dmp${suffix}`, password: `pw-${suffix}-dmp-x1`, registered: false }

async function joinPolishMember(owner: Page, page: Page): Promise<string> {
  await page.goto('/')
  if (polish.registered) {
    expect((await apiFetch(page, 'POST', '/api/auth/login', { username: polish.username, password: polish.password })).status).toBe(200)
  } else {
    const invite = await apiFetch(owner, 'POST', '/api/admin/invites', { max_uses: 1 })
    expect((await apiFetch(page, 'POST', '/api/auth/register', { username: polish.username, password: polish.password, invite_code: responseString(invite, 'code') })).status).toBe(201)
    polish.registered = true
  }
  const id = responseUserId(await apiFetch(page, 'GET', '/api/auth/me'))
  await page.goto('/')
  return id
}

/**
 * The conversation of two. This spec's own member starts it: the Owner's few starts per minute
 * are what dm.spec.ts needs, and starting counts against them.
 */
async function startDm(starter: Page, otherId: string): Promise<string> {
  const mine = (await apiFetch(starter, 'GET', '/api/dms')).json as { id: string; group: boolean; members: { id: string }[] }[]
  const existing = mine.find(dm => !dm.group && dm.members.some(m => m.id === otherId))
  if (existing) return existing.id
  const started = await apiFetch(starter, 'POST', '/api/dms', { user_ids: [otherId] })
  expect([200, 201]).toContain(started.status)
  return responseString(started, 'id')
}

const rowOf = (page: Page, id: string) => page.locator(`[data-dm="${id}"]`)

async function resetList(member: Page, dmId: string) {
  await apiFetch(member, 'PUT', '/api/dms/favourites', { ids: [] })
  await apiFetch(member, 'PUT', `/api/dms/${dmId}/hidden`, { hidden: false })
  await apiFetch(member, 'PUT', '/api/users/me/dm-settings', { mute_all: false, list_collapsed: false })
}

test('the list folds, closes a conversation until the next message, and keeps favourites', async ({ browser }) => {
  const ownerCtx = await browser.newContext({ viewport: VIEWPORT })
  const memberCtx = await browser.newContext({ viewport: VIEWPORT })
  const owner = await ownerCtx.newPage()
  const member = await memberCtx.newPage()
  let dmId = ''
  try {
    await signIn(owner, ADMIN_USER, ADMIN_PASSWORD)
    const ownerId = responseUserId(await apiFetch(owner, 'GET', '/api/auth/me'))
    await joinPolishMember(owner, member)
    dmId = await startDm(member, ownerId)
    expect((await apiFetch(owner, 'POST', `/api/channels/${dmId}/messages`, { content: `erste ${suffix}` })).status).toBe(201)
    await member.goto('/')
    await expect(rowOf(member, dmId)).toBeVisible()
    const health = await apiFetch(member, 'GET', '/api/health')
    expect((health.json as { features?: string[] }).features).toContain('dm.v2')

    // The preview names the latest message; the section folds away completely and stays folded on reload.
    await expect(rowOf(member, dmId).getByTestId('dm-preview')).toHaveText(`erste ${suffix}`)
    await member.getByTestId('dm-toggle').click()
    await expect(member.getByTestId('dm-body')).toHaveCount(0)
    await member.reload()
    await expect(member.getByTestId('dm-toggle')).toHaveAttribute('aria-expanded', 'false')
    await expect(rowOf(member, dmId)).toHaveCount(0)
    await member.getByTestId('dm-toggle').click()
    await expect(rowOf(member, dmId)).toBeVisible()

    // Closing hides the row (nothing is deleted); the next message of the other side brings it back, live.
    await rowOf(member, dmId).hover()
    await rowOf(member, dmId).getByTestId('dm-close').click()
    await expect(rowOf(member, dmId)).toHaveCount(0)
    await member.reload()
    await expect(member.getByTestId('dm-section')).toBeVisible()
    await expect(rowOf(member, dmId)).toHaveCount(0)
    expect((await apiFetch(owner, 'POST', `/api/channels/${dmId}/messages`, { content: `zweite ${suffix}` })).status).toBe(201)
    await expect(rowOf(member, dmId)).toBeVisible()
    await expect(rowOf(member, dmId).getByTestId('dm-preview')).toHaveText(`zweite ${suffix}`)

    // A favourite goes to its own section and is still there after a reload (it is kept on the server).
    await rowOf(member, dmId).hover()
    await rowOf(member, dmId).getByTestId('dm-star').click()
    await expect(member.getByTestId('dm-favourites').locator(`[data-dm="${dmId}"]`)).toBeVisible()
    await member.reload()
    await expect(member.getByTestId('dm-favourites').locator(`[data-dm="${dmId}"]`)).toBeVisible()
    await rowOf(member, dmId).hover()
    await expect(rowOf(member, dmId).getByTestId('dm-close')).toHaveCount(0)
    await rowOf(member, dmId).getByTestId('dm-star').click()
    await expect(member.getByTestId('dm-favourites')).toHaveCount(0)
    await expect(rowOf(member, dmId)).toBeVisible()
  } finally {
    if (dmId) await resetList(member, dmId)
    await ownerCtx.close()
    await memberCtx.close()
  }
})

test('muting all direct messages keeps the unread count but drops sound and highlight', async ({ browser }) => {
  const ownerCtx = await browser.newContext({ viewport: VIEWPORT })
  const memberCtx = await browser.newContext({ viewport: VIEWPORT })
  const owner = await ownerCtx.newPage()
  const member = await memberCtx.newPage()
  let dmId = ''
  try {
    await signIn(owner, ADMIN_USER, ADMIN_PASSWORD)
    const ownerId = responseUserId(await apiFetch(owner, 'GET', '/api/auth/me'))
    await joinPolishMember(owner, member)
    dmId = await startDm(member, ownerId)
    await member.goto('/')
    await expect(rowOf(member, dmId)).toBeVisible()

    // The switch in the settings is the member's own and reaches the list at once.
    await member.getByTestId('account-menu-button').click()
    await member.getByTestId('menu-settings').click()
    await member.getByTestId('settings-tab-privacy').click()
    await member.getByTestId('dm-mute-all').click()
    await expect(member.getByTestId('dm-mute-all')).toHaveAttribute('aria-checked', 'true')
    await member.keyboard.press('Escape')
    await expect(rowOf(member, dmId).getByTestId('dm-muted')).toBeVisible()

    // Unread still counts, as a quiet number, and a mention does not stand out.
    // (The conversation's starter may write only twice before anybody answers, so the member answers first.)
    expect((await apiFetch(member, 'POST', `/api/channels/${dmId}/messages`, { content: `hallo ${suffix}` })).status).toBe(201)
    expect((await apiFetch(member, 'POST', `/api/channels/${dmId}/read`, {})).status).toBe(204)
    const mention = (await apiFetch(owner, 'POST', `/api/channels/${dmId}/messages`, { content: `@${polish.username} hallo ${suffix}` }))
    expect(mention.status).toBe(201)
    const mine = async () => ((await apiFetch(member, 'GET', '/api/dms')).json as { id: string; unread_count: number; mention_count: number; notify_level: string }[]).find(dm => dm.id === dmId)
    await expect.poll(async () => (await mine())?.unread_count).toBe(1)
    expect(await mine()).toMatchObject({ mention_count: 0, notify_level: 'mute' })

    // The conversation's own setting wins over the general one.
    expect((await apiFetch(member, 'PUT', `/api/channels/${dmId}/notifications`, { level: 'all', muted_until: null })).status).toBe(204)
    await member.reload()
    await expect(rowOf(member, dmId).getByTestId('dm-muted')).toHaveCount(0)
  } finally {
    if (dmId) await resetList(member, dmId)
    if (dmId) await apiFetch(member, 'PUT', `/api/channels/${dmId}/notifications`, { level: 'inherit', muted_until: null })
    await ownerCtx.close()
    await memberCtx.close()
  }
})
