import { test, expect } from './fixtures'
import type { Page } from '@playwright/test'
import { apiFetch } from './helpers'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'cool_guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
const PIXEL = 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAIAAACQd1PeAAAADElEQVR4nGOQW1ABAAIWATe4c9leAAAAAElFTkSuQmCC'
const TITLE = `Raid rules ${suffix}`

async function signIn(page: Page) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByTestId('login-heading')).toBeVisible()
  await dialog.getByLabel('Benutzername').fill(ADMIN_USER)
  await dialog.getByLabel('Passwort').fill(ADMIN_PASSWORD)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

test('an owner writes a wiki page, edits it, reads the history and restores the first version', async ({ page }) => {
  await signIn(page)
  await page.getByTestId('sidebar-wiki').click()
  await expect(page).toHaveURL(/\/wiki$/)

  // Create.
  await page.getByTestId('wiki-new-top').click()
  await page.getByTestId('wiki-title-input').fill(TITLE)
  await page.getByTestId('wiki-text-input').fill('# Loot\n\nNeed before greed.')
  await expect(page.getByTestId('wiki-preview')).toContainText('Need before greed.')
  // A picture: uploaded, redrawn by the server, put into the text and shown.
  await page.getByTestId('wiki-image-input').setInputFiles({ name: 'dot.png', mimeType: 'image/png', buffer: Buffer.from(PIXEL, 'base64') })
  await expect(page.getByTestId('wiki-text-input')).toHaveValue(/!\[dot\]\(\/api\/media\/[0-9a-f-]{36}\)/)
  await expect(page.getByTestId('wiki-preview').locator('img.md-wikiimage')).toBeVisible()
  await page.getByTestId('wiki-save').click()
  await expect(page.getByTestId('wiki-title')).toHaveText(TITLE)
  await expect(page.getByTestId('wiki-content')).toContainText('Need before greed.')
  await expect(page.getByTestId('wiki-content').locator('img.md-wikiimage')).toHaveCount(1)
  const slug = new URL(page.url()).pathname.split('/')[2] ?? ''
  expect(slug).toMatch(/^raid-rules-/)

  // Edit: a second version with a summary.
  await page.getByTestId('wiki-edit').click()
  await page.getByTestId('wiki-text-input').fill('# Loot\n\nMain spec first.')
  await page.getByTestId('wiki-summary-input').fill('new loot rule')
  await page.getByTestId('wiki-save').click()
  await expect(page.getByTestId('wiki-content')).toContainText('Main spec first.')

  // History: both versions, the change as a diff.
  await page.getByTestId('wiki-history').click()
  await expect(page.getByTestId('wiki-revisions').locator('[data-rev]')).toHaveCount(2)
  await expect(page.getByTestId('wiki-revisions')).toContainText('new loot rule')
  await expect(page.locator('[data-testid="wiki-diff"] [data-op="add"]')).toContainText('Main spec first.')
  await expect(page.locator('[data-testid="wiki-diff"] [data-op="del"]')).toContainText('Need before greed.')

  // Restore the first version: a second click confirms, and it becomes version 3.
  await page.locator('[data-rev="1"]').click()
  await page.getByTestId('wiki-restore').click()
  await page.getByRole('alertdialog').getByRole('button', { name: 'Wiederherstellen' }).click()
  await expect(page.getByTestId('wiki-content')).toContainText('Need before greed.')
  const page3 = await apiFetch(page, 'GET', `/api/wiki/pages/${slug}`)
  expect(page3.status).toBe(200)
  expect((page3.json as { rev: number }).rev).toBe(3)

  // Search finds it; delete and bring it back.
  await page.getByTestId('wiki-search-input').fill('greed')
  await expect(page.getByTestId('wiki-hit').filter({ hasText: TITLE })).toBeVisible()
  await page.getByTestId('wiki-hit').filter({ hasText: TITLE }).click()
  await page.getByTestId('wiki-delete').click()
  await page.getByRole('alertdialog').getByRole('button', { name: 'Löschen' }).click()
  await expect(page.getByTestId('wiki-start')).toBeVisible()
  await page.getByTestId('wiki-deleted').click()
  await page.locator(`[data-restore="${slug}"]`).click()
  await page.getByTestId('wiki-home').click()
  await expect(page.getByTestId('wiki-tree').locator(`[data-wiki-page="${slug}"]`)).toBeVisible()
})
