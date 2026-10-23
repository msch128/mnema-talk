import { test, expect } from './fixtures'
import type { Page } from '@playwright/test'
import { apiFetch, responseString, suiteMember } from './helpers'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'Herzog'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
const CATEGORY = `Ruhe ${suffix}`
const HOME = `zuhause-${suffix}`
const ONE = `leise-${suffix}`
const TWO = `laut-${suffix}`

async function signIn(page: Page, username: string, password: string) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByRole('heading', { name: 'Mnema Talk' })).toBeVisible()
  await dialog.getByLabel('Benutzername').fill(username)
  await dialog.getByLabel('Passwort').fill(password)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

async function settings(page: Page) {
  const res = await apiFetch(page, 'GET', '/api/notification-settings') as { status: number; json: { channels: Array<{ channel_id: string; level: string; muted_until: string | null }>; categories: Array<{ category_id: string; level: string }> } }
  expect(res.status).toBe(200)
  return res.json
}

test('notification levels, timed mutes, category badges and mark all read', async ({ browser }) => {
  const ownerCtx = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  const memberCtx = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  const owner = await ownerCtx.newPage()
  const member = await memberCtx.newPage()
  // Everything made here is removed again: the sidebar specs that follow read the whole list.
  const created: string[] = []
  let categoryToDelete = ''
  try {
    await signIn(owner, ADMIN_USER, ADMIN_PASSWORD)
    const category = await apiFetch(owner, 'POST', '/api/admin/categories', { name: CATEGORY })
    expect(category.status).toBe(201)
    const categoryId = responseString(category, 'id')
    categoryToDelete = categoryId
    const make = async (name: string, categoryId?: string) => {
      const res = await apiFetch(owner, 'POST', '/api/admin/channels', { name, type: 'text', ...(categoryId ? { category_id: categoryId } : {}) })
      expect(res.status).toBe(201)
      const id = responseString(res, 'id')
      created.push(id)
      return id
    }
    await make(HOME)
    const one = await make(ONE, categoryId)
    const two = await make(TWO, categoryId)

    await member.goto('/')
    if (suiteMember.registered) {
      expect((await apiFetch(member, 'POST', '/api/auth/login', { username: suiteMember.username, password: suiteMember.password })).status).toBe(200)
    } else {
      const invite = await apiFetch(owner, 'POST', '/api/admin/invites', { max_uses: 1 })
      expect((await apiFetch(member, 'POST', '/api/auth/register', { username: suiteMember.username, password: suiteMember.password, invite_code: responseString(invite, 'code') })).status).toBe(201)
      suiteMember.registered = true
    }
    await member.reload()
    await member.locator('[data-channel-type="text"]', { hasText: HOME }).click()

    // News in the category: a collapsed category shows a dot, then the mentions.
    const post = (channel: string, content: string) => apiFetch(owner, 'POST', `/api/channels/${channel}/messages`, { content })
    expect((await post(one, 'hallo eins')).status).toBe(201)
    const toggle = member.locator(`[data-category-toggle="${categoryId}"]`)
    await expect(member.locator(`[data-channel-item="${one}"] [data-testid="channel-unread"]`)).toHaveText('1')
    await toggle.click()
    await expect(member.getByTestId('category-unread')).toBeVisible()
    await expect(member).toHaveTitle(/^• /)
    expect((await post(two, `hallo @${suiteMember.username}`)).status).toBe(201)
    await expect(member.getByTestId('category-mentions')).toHaveText('1')
    await expect(member).toHaveTitle(/^\(1\) /)
    await toggle.click()

    // A timed mute on one channel: stored with an end, shown dimmed with a bell.
    await member.locator(`[data-drop="channel"][data-id="${one}"]`).click({ button: 'right' })
    await member.getByRole('menuitem', { name: 'Kanal stummschalten' }).click()
    await member.getByRole('menuitem', { name: 'Für 1 Stunde' }).click()
    await expect(member.locator(`[data-channel-item="${one}"] [data-testid="channel-muted"]`)).toBeVisible()
    await expect(member.locator(`[data-channel-item="${one}"] [data-testid="channel-unread"]`)).toHaveCount(0)
    // The bell appears at once; the server has the setting a moment later.
    const storedMute = async () => (await settings(member)).channels.find((c) => c.channel_id === one)
    await expect.poll(async () => (await storedMute())?.level).toBe('mute')
    const mute = await storedMute()
    expect(Date.parse(mute?.muted_until ?? '') - Date.now()).toBeGreaterThan(50 * 60_000)

    // A past or too distant end is refused by the server.
    const past = await apiFetch(member, 'PUT', `/api/channels/${one}/notifications`, { level: 'mute', muted_until: new Date(Date.now() - 60_000).toISOString() })
    expect(past.status).toBe(400)

    // Unmuting returns to the category's setting.
    await member.locator(`[data-drop="channel"][data-id="${one}"]`).click({ button: 'right' })
    await member.getByRole('menuitem', { name: 'Stummschaltung aufheben' }).click()
    await expect(member.locator(`[data-channel-item="${one}"] [data-testid="channel-muted"]`)).toHaveCount(0)

    // The category's own level: channels without a setting follow it.
    await member.locator(`[data-drop="header"][data-id="${categoryId}"]`).click({ button: 'right' })
    await member.getByRole('menuitem', { name: 'Kategorie stummschalten' }).click()
    await member.getByRole('menuitem', { name: 'Bis ich es wieder einschalte' }).click()
    await expect(member.locator(`[data-channel-item="${two}"] [data-testid="channel-muted"]`)).toBeVisible()
    expect((await settings(member)).categories.some((c) => c.category_id === categoryId && c.level === 'mute')).toBe(true)

    // Mark everything read from the server menu: no badges, no count in the title.
    await member.getByRole('button', { name: 'Mnema Talk' }).click()
    await member.getByRole('menuitem', { name: 'Alle als gelesen markieren' }).click()
    await expect(member.getByTestId('channel-mentions')).toHaveCount(0)
    await expect(member.getByTestId('channel-unread')).toHaveCount(0)
    await expect(member).not.toHaveTitle(/^(\(\d+\)|•) /)
  } finally {
    for (const id of created) await apiFetch(owner, 'DELETE', `/api/admin/channels/${id}`)
    if (categoryToDelete) await apiFetch(owner, 'DELETE', `/api/admin/categories/${categoryToDelete}`)
    await ownerCtx.close()
    await memberCtx.close()
  }
})
