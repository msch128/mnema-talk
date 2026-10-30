import { test, expect } from './fixtures'
import type { Browser, Page } from '@playwright/test'
import { apiFetch, responseString, suiteMember } from './helpers'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'cool_guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
const BUSY = `lang-${suffix}`
const QUIET = `ruhig-${suffix}`
// More than one page of 50, so the first unread message is not on the page that loads.
const NEWS = 70

async function signIn(page: Page, username: string, password: string) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByTestId('login-heading')).toBeVisible()
  await dialog.getByLabel('Benutzername').fill(username)
  await dialog.getByLabel('Passwort').fill(password)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

interface UnreadRun { owner: Page; member: Page; busy: string }

// An owner and a member; the member opened BUSY once and then NEWS messages arrived, which the member has not seen.
async function withUnreadRun(browser: Browser, body: (run: UnreadRun) => Promise<void>) {
  const ownerCtx = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  const memberCtx = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  const owner = await ownerCtx.newPage()
  const member = await memberCtx.newPage()
  const created: string[] = []
  try {
    await signIn(owner, ADMIN_USER, ADMIN_PASSWORD)
    const make = async (name: string) => {
      const res = await apiFetch(owner, 'POST', '/api/admin/channels', { name, type: 'text' })
      expect(res.status).toBe(201)
      created.push(responseString(res, 'id'))
      return responseString(res, 'id')
    }
    const busy = await make(BUSY)
    await make(QUIET)

    await member.goto('/')
    if (suiteMember.registered) {
      expect((await apiFetch(member, 'POST', '/api/auth/login', { username: suiteMember.username, password: suiteMember.password })).status).toBe(200)
    } else {
      const invite = await apiFetch(owner, 'POST', '/api/admin/invites', { max_uses: 1 })
      expect((await apiFetch(member, 'POST', '/api/auth/register', { username: suiteMember.username, password: suiteMember.password, invite_code: responseString(invite, 'code') })).status).toBe(201)
      suiteMember.registered = true
    }
    await member.reload()

    // Open the channel once (it starts the read marker), then leave it.
    await member.locator('[data-channel-type="text"]', { hasText: BUSY }).click()
    await member.locator('[data-channel-type="text"]', { hasText: QUIET }).click()

    for (let i = 1; i <= NEWS; i++) {
      expect((await apiFetch(owner, 'POST', `/api/channels/${busy}/messages`, { content: `news ${i}` })).status).toBe(201)
    }
    await body({ owner, member, busy })
  } finally {
    for (const id of created) await apiFetch(owner, 'DELETE', `/api/admin/channels/${id}`)
    await ownerCtx.close()
    await memberCtx.close()
  }
}

async function unreadCount(member: Page, channel: string) {
  const state = await apiFetch(member, 'GET', '/api/read-state') as { json: Array<{ channel_id: string; unread_count: number }> }
  return state.json.find((s) => s.channel_id === channel)?.unread_count ?? 0
}

test('a long unread run: jump to the first unread message, divider, Esc marks read', async ({ browser }) => {
  await withUnreadRun(browser, async ({ member, busy }) => {
    await member.locator('[data-channel-type="text"]', { hasText: BUSY }).click()
    const pill = member.getByTestId('jump-to-unread')
    await expect(pill).toBeVisible()
    await expect(member.getByText(`news ${NEWS}`, { exact: true })).toBeVisible()

    await pill.click()
    const first = member.locator('[data-msg-id]', { hasText: 'news 1' }).first()
    await expect(member.locator('[data-unread-divider]')).toBeVisible()
    await expect(first).toBeInViewport()
    await expect(pill).toHaveCount(0)

    // Esc in the channel marks it read and drops the divider.
    await member.keyboard.press('Escape')
    await expect(member.locator('[data-unread-divider]')).toHaveCount(0)
    expect(await unreadCount(member, busy)).toBe(0)
  })
})

test('sending a message reads the channel: no "Jump to first unread" afterwards, and the server agrees', async ({ browser }) => {
  await withUnreadRun(browser, async ({ member, busy }) => {
    await member.locator('[data-channel-type="text"]', { hasText: BUSY }).click()
    const pill = member.getByTestId('jump-to-unread')
    await expect(pill).toBeVisible()

    const composer = member.getByPlaceholder(/^Nachricht an /)
    await composer.fill(`my answer ${suffix}`)
    await composer.press('Enter')
    await expect(member.getByText(`my answer ${suffix}`, { exact: true })).toBeVisible()
    await expect(pill).toHaveCount(0)
    await expect(member.locator('[data-unread-divider]')).toHaveCount(0)
    expect(await unreadCount(member, busy)).toBe(0)
  })
})
