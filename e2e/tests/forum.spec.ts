import { mkdir } from 'node:fs/promises'
import { test, expect } from './fixtures'
import type { Page } from '@playwright/test'
import { apiFetch, responseString } from './helpers'

// The forum end to end: a moderator defines the tags and the rules, a post is
// created with tags, its tags are edited afterwards, the list is filtered by
// tag and searched, and a post is marked solved.

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'cool_guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
const SHOTS = process.env.UI_SHOTS_DIR
const SHOT_PREFIX = process.env.UI_SHOTS_PREFIX ?? ''

async function signIn(page: Page) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByTestId('login-heading')).toBeVisible()
  await dialog.getByLabel('Benutzername').fill(ADMIN_USER)
  await dialog.getByLabel('Passwort').fill(ADMIN_PASSWORD)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

async function shot(page: Page, name: string) {
  if (!SHOTS) return
  await mkdir(SHOTS, { recursive: true })
  await page.waitForTimeout(400) // let dialogs and fades settle
  await page.screenshot({ path: `${SHOTS}/${SHOT_PREFIX}${name}.png` })
}

async function addTag(page: Page, name: string, color: string) {
  const dialog = page.getByRole('dialog')
  await dialog.getByTestId('forum-add-tag').click()
  const editor = dialog.getByTestId('forum-tag-editor')
  await editor.getByTestId('forum-tag-name').fill(name)
  await editor.getByRole('radio', { name: color }).click()
  await editor.getByTestId('forum-tag-save').click()
  await expect(dialog.getByTestId('forum-tag-row').filter({ hasText: name })).toBeVisible()
}

for (const viewport of [{ width: 1440, height: 900, label: 'desktop' }, { width: 390, height: 844, label: 'mobile' }]) {
  test(`a forum with tags: create a post with tags, edit the tags, filter, search and solve at ${viewport.label}`, async ({ browser }) => {
    const ctx = await browser.newContext({ viewport, hasTouch: viewport.width < 768 })
    const page = await ctx.newPage()
    const label = viewport.label
    try {
      await signIn(page)
      const forum = await apiFetch(page, 'POST', '/api/admin/channels', { name: `hilfe-${suffix}-${label}`, type: 'forum' })
      expect(forum.status).toBe(201)
      const forumId = responseString(forum, 'id')
      await page.goto(`/c/${forumId}`)
      await expect(page.getByTestId('forum-area')).toBeVisible()

      // Empty forum: says what a forum is and how to start.
      await expect(page.getByTestId('forum-empty')).toBeVisible()
      await shot(page, `forum-${label}-1-empty`)

      // The moderator defines the tags and the guidelines.
      await page.getByTestId('manage-tags-btn').click()
      const manage = page.getByRole('dialog')
      await addTag(page, 'Fehler', 'Rot')
      await addTag(page, 'Frage', 'Blau')
      await manage.getByLabel('Richtlinien').fill('Bitte erst die Suche benutzen.')
      await manage.getByTestId('forum-settings-save').click()
      await expect(manage.getByTestId('forum-settings-save')).toBeDisabled()
      await shot(page, `forum-${label}-2-manage-tags`)
      await manage.getByRole('button', { name: 'Schließen' }).last().click()
      await expect(manage).toBeHidden()

      // A post with a tag; the guidelines are shown in the dialog.
      await page.getByTestId('new-post-btn').click()
      const create = page.getByRole('dialog')
      await expect(create.getByTestId('forum-guidelines')).toContainText('Bitte erst die Suche benutzen.')
      await create.getByLabel('Titel des Beitrags').fill('Login hängt in Safari')
      await create.getByTestId('tag-toggle-btn').filter({ hasText: 'Fehler' }).click()
      await create.getByLabel('Erste Nachricht').fill('Nach dem Anmelden passiert **nichts**.')
      await shot(page, `forum-${label}-3-create`)
      await create.getByTestId('submit-post').click()
      await expect(create).toBeHidden()

      // The new post is open; its header carries the tag, and the tags can be edited.
      const header = page.getByTestId('forum-post-header')
      await expect(header.getByTestId('forum-post-title')).toHaveText('Login hängt in Safari')
      await expect(header.getByTestId('forum-header-tag')).toHaveText(['Fehler'])
      await header.getByTestId('forum-edit-tags').click()
      const editor = header.getByTestId('forum-tag-edit')
      await editor.getByTestId('tag-toggle-btn').filter({ hasText: 'Frage' }).click()
      await editor.getByTestId('tag-toggle-btn').filter({ hasText: 'Fehler' }).click()
      await editor.getByTestId('forum-tags-save').click()
      await expect(header.getByTestId('forum-header-tag')).toHaveText(['Frage'])
      await shot(page, `forum-${label}-4-post`)

      // The change reached the server: the list card shows it after a reload.
      const second = await apiFetch(page, 'POST', `/api/channels/${forumId}/posts`, { title: 'Zweiter Beitrag', content: 'Etwas anderes', tag_ids: [] })
      expect(second.status).toBe(201)
      await page.goto(`/c/${forumId}`)
      const cards = page.getByTestId('forum-post-card')
      await expect(cards).toHaveCount(2)
      await expect(cards.filter({ hasText: 'Login hängt in Safari' }).getByTestId('forum-post-tag')).toHaveText(['Frage'])

      // Filter by tag, then by search.
      await page.getByTestId('filter-tag-pill').filter({ hasText: 'Frage' }).click()
      await expect(cards).toHaveCount(1)
      await expect(cards.first()).toContainText('Login hängt in Safari')
      await shot(page, `forum-${label}-5-filtered`)
      await page.getByTestId('filter-all-tags').click()
      await expect(cards).toHaveCount(2)
      await page.getByTestId('forum-search').fill('zweiter')
      await expect(cards).toHaveCount(1)
      await expect(cards.first()).toContainText('Zweiter Beitrag')
      await page.getByTestId('forum-search').fill('gibt es nicht')
      await expect(page.getByTestId('forum-empty-filtered')).toBeVisible()
      await page.getByRole('button', { name: 'Alle Beiträge anzeigen' }).click()
      await expect(cards).toHaveCount(2)

      // Mark solved from the post's menu; the card shows it.
      await cards.filter({ hasText: 'Login hängt in Safari' }).click()
      await page.getByTestId('forum-post-menu').click()
      await page.getByRole('menuitem', { name: 'Als gelöst markieren' }).click()
      await expect(page.getByTestId('forum-post-header').getByTestId('forum-post-solved')).toBeVisible()
      await expect(cards.filter({ hasText: 'Login hängt in Safari' }).getByTestId('forum-post-solved')).toBeVisible()
      await shot(page, `forum-${label}-6-list`)
    } finally {
      await ctx.close()
    }
  })
}
