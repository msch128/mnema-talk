import { test, expect } from './fixtures'
import type { Page } from '@playwright/test'

// A weekly event: the next dates appear on their own, "this and following"
// changes them together, and cancelling the following ones ends the series.

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

function localInput(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

test('a weekly event creates its next dates, an edit of the following ones changes them together and cancelling them ends the series', async ({ page }) => {
  const title = `Stammtisch ${suffix}`
  await signIn(page)
  await page.getByRole('button', { name: 'Veranstaltungen' }).click()
  await page.getByTestId('events-empty-create').or(page.getByRole('button', { name: 'Veranstaltung erstellen' }).first()).click()

  const create = page.getByRole('dialog').last()
  await create.getByLabel('Titel', { exact: true }).fill(title)
  await create.locator('input[type="datetime-local"]').first().fill(localInput(new Date(Date.now() + 3 * 86_400_000)))
  await create.getByLabel('Freitext-Ort').fill('Stammkneipe')
  await create.getByTestId('event-repeat-select').selectOption('weekly')
  await create.getByRole('button', { name: 'Erstellen' }).click()
  await expect(create).toBeHidden()

  // The server created the dates of the next weeks (60 days ahead: eight or nine of them).
  const cards = page.locator('article').filter({ hasText: title })
  await expect(cards.first()).toBeVisible()
  await expect.poll(() => cards.count()).toBeGreaterThanOrEqual(8)
  await expect(cards.first().getByTestId('repeat-badge')).toContainText('Jede Woche')

  // Rename this and the following ones from the second date on: the first keeps its name.
  await cards.nth(1).getByRole('button', { name: 'Details' }).click()
  await page.getByRole('button', { name: 'Veranstaltung bearbeiten' }).click()
  const edit = page.getByRole('dialog').last()
  await edit.getByLabel('Titel', { exact: true }).fill(`${title} neu`)
  await edit.getByTestId('scope-following').check()
  await edit.getByRole('button', { name: 'Änderungen speichern' }).click()
  await expect(edit).toBeHidden()
  await expect(page.locator('article').filter({ hasText: `${title} neu` }).first()).toBeVisible()
  await expect(page.locator('article').filter({ hasText: title }).filter({ hasNotText: 'neu' })).toHaveCount(1)

  // Cancel the following ones from the third date on: the series ends there.
  const renamed = page.locator('article').filter({ hasText: `${title} neu` })
  await renamed.nth(1).getByRole('button', { name: 'Details' }).click()
  await page.getByRole('button', { name: 'Veranstaltung absagen' }).click()
  await page.getByTestId('scope-following').check()
  await page.getByTestId('cancel-confirm').click()
  await expect.poll(() => renamed.count()).toBe(1)
})
