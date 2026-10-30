import { test, expect } from './fixtures'
import type { Page } from '@playwright/test'
import { apiFetch, responseString } from './helpers'

// A forum post closes with the X button and with Escape, the address goes back
// to the forum, and a reload does not open the post again.

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'cool_guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
const FORUM = `close-forum-${suffix}`
const POST_TITLE = `Close me ${suffix}`
const POST_PATH = /\/p\/close-me-[a-z0-9-]+$/

async function signIn(page: Page) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByTestId('login-heading')).toBeVisible()
  await dialog.getByLabel('Benutzername').fill(ADMIN_USER)
  await dialog.getByLabel('Passwort').fill(ADMIN_PASSWORD)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

async function expectClosedForum(page: Page) {
  await expect(page.getByTestId('thread-close')).toBeHidden()
  await expect(page).not.toHaveURL(POST_PATH)
  await expect(page.getByTestId('forum-area')).toBeVisible()
  // Still closed a moment later: nothing re-opens it.
  await page.waitForTimeout(500)
  await expect(page.getByTestId('thread-close')).toBeHidden()
  await page.reload()
  await expect(page.getByTestId('forum-area')).toBeVisible()
  await expect(page.getByTestId('thread-close')).toBeHidden()
}

test('a forum post closes with X and with Escape, and stays closed after a reload', async ({ browser }) => {
  const context = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  const page = await context.newPage()
  let forumId = ''
  try {
    await signIn(page)
    const forum = await apiFetch(page, 'POST', '/api/admin/channels', { name: FORUM, type: 'forum' })
    expect(forum.status).toBe(201)
    forumId = responseString(forum, 'id')
    expect((await apiFetch(page, 'POST', `/api/channels/${forumId}/posts`, { title: POST_TITLE, content: 'Hello forum' })).status).toBe(201)

    await page.goto(`/c/${forumId}`)
    const card = page.getByTestId('forum-post-card').filter({ hasText: POST_TITLE })

    await card.click()
    await expect(page.getByTestId('thread-close')).toBeVisible()
    await expect(page).toHaveURL(POST_PATH)
    await page.getByTestId('thread-close').click()
    await expectClosedForum(page)

    await page.getByTestId('forum-post-card').filter({ hasText: POST_TITLE }).click()
    await expect(page).toHaveURL(POST_PATH)
    await page.keyboard.press('Escape')
    await expectClosedForum(page)
  } finally {
    if (forumId) await apiFetch(page, 'DELETE', `/api/admin/channels/${forumId}`).catch(() => undefined)
    await context.close()
  }
})
