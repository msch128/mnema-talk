import { test, expect } from './fixtures'
import type { Page } from '@playwright/test'
import { apiFetch, responseString } from './helpers'

// Two sessions edit the same page (doc/wiki.md): the second save names a
// revision that is no longer the latest, the server refuses it with
// STALE_REVISION, and the editor offers to keep the writer's text on top of the
// newer version or to take the newer one. Nothing is overwritten either way.

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'cool_guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)

async function signIn(page: Page) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByTestId('login-heading')).toBeVisible()
  await dialog.getByLabel('Benutzername').fill(ADMIN_USER)
  await dialog.getByLabel('Passwort').fill(ADMIN_PASSWORD)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

/** Both sessions open the editor of the page; the first saves; the second is refused. */
async function conflict(first: Page, second: Page, slug: string, firstText: string, secondText: string) {
  for (const page of [first, second]) {
    await page.goto(`/wiki/${slug}/edit`)
    await expect(page.getByTestId('wiki-editor')).toBeVisible()
  }
  await first.getByTestId('wiki-text-input').fill(firstText)
  await first.getByTestId('wiki-save').click()
  await expect(first.getByTestId('wiki-content')).toContainText(firstText)

  await second.getByTestId('wiki-text-input').fill(secondText)
  await second.getByTestId('wiki-save').click()
  await expect(second.getByTestId('wiki-conflict')).toBeVisible()
  await expect(second.getByTestId('wiki-conflict-mine')).toBeVisible()
  await expect(second.getByTestId('wiki-conflict-theirs')).toBeVisible()
}

test('a save on an outdated revision is refused and the writer keeps their text on top or takes the newer one', async ({ browser }) => {
  const aCtx = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  const bCtx = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  const a = await aCtx.newPage()
  const b = await bCtx.newPage()
  const slugs: string[] = []
  try {
    await signIn(a)
    await signIn(b)
    const pages = []
    for (const name of ['mine', 'theirs']) {
      const created = await apiFetch(a, 'POST', '/api/wiki/pages', { title: `Conflict ${name} ${suffix}`, content: 'start' })
      expect(created.status).toBe(201)
      pages.push(responseString(created, 'slug'))
      slugs.push(responseString(created, 'slug'))
    }
    const [keepMine, takeTheirs] = pages
    if (!keepMine || !takeTheirs) throw new Error('pages were not created')

    // The API says so in a stable code, with the page unchanged.
    const stale = await apiFetch(b, 'PATCH', `/api/wiki/pages/${keepMine}`, { base_rev: 1, content: 'x' })
    expect(stale.status).toBe(200)
    const refused = await apiFetch(a, 'PATCH', `/api/wiki/pages/${keepMine}`, { base_rev: 1, content: 'y' })
    expect(refused.status).toBe(409)
    expect((refused.json as { error?: { code?: string } }).error?.code).toBe('STALE_REVISION')

    // Take theirs: the newer version is shown and nothing of the second writer is saved.
    await conflict(a, b, takeTheirs, 'first writer', 'second writer')
    await b.getByTestId('wiki-conflict-theirs').click()
    await b.getByRole('alertdialog').getByRole('button', { name: 'Verwerfen' }).click()
    await expect(b.getByTestId('wiki-content')).toContainText('first writer')
    const kept = await apiFetch(a, 'GET', `/api/wiki/pages/${takeTheirs}`)
    expect((kept.json as { rev: number; content: string }).rev).toBe(2)
    expect((kept.json as { content: string }).content).toBe('first writer')

    // Keep mine: the second session's text is saved on top, the first one's stays in the history.
    await conflict(a, b, keepMine, 'third writer', 'fourth writer')
    await b.getByTestId('wiki-conflict-mine').click()
    await expect(b.getByTestId('wiki-content')).toContainText('fourth writer')
    const now = await apiFetch(a, 'GET', `/api/wiki/pages/${keepMine}`)
    expect((now.json as { rev: number; content: string }).content).toBe('fourth writer')
    const revisions = await apiFetch(a, 'GET', `/api/wiki/pages/${keepMine}/revisions`)
    expect((revisions.json as unknown[]).length).toBe(4)
  } finally {
    for (const slug of slugs) await apiFetch(a, 'DELETE', `/api/wiki/pages/${slug}`)
    await aCtx.close()
    await bCtx.close()
  }
})
