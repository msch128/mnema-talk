import type { Page } from '@playwright/test'
import { apiFetch, awaitApiBudget, responseString } from './helpers'
import { test, expect } from './fixtures'

// Talk settings (doc/settings-ux.md): one draft, one Save. Nothing is stored
// before Save, "changed" means different after normalising, and closing with
// unsaved changes asks first. No call is needed.

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'cool_guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const NAME = `Settings ${Date.now().toString(36)}`

async function signIn(page: Page) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('Benutzername').fill(ADMIN_USER)
  await dialog.getByLabel('Passwort').fill(ADMIN_PASSWORD)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

async function storedTalk(page: Page, id: string) {
  const hierarchy = (await apiFetch(page, 'GET', '/api/channels')).json
  const all = [...hierarchy.uncategorized, ...hierarchy.categories.flatMap(c => c.channels)]
  const found = all.find(c => c.id === id)
  if (!found) throw new Error(`channel ${id} is gone`)
  return found
}

test('the Talk settings save only through Save, with exact change detection and a discard prompt', async ({ browser }) => {
  const context = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  const page = await context.newPage()
  await signIn(page)
  if (await awaitApiBudget(page)) await page.reload()
  const created = await apiFetch(page, 'POST', '/api/admin/channels', { name: NAME, type: 'voice' })
  expect(created.status).toBe(201)
  const id = responseString(created, 'id')
  await page.reload()

  const openSettings = async () => {
    await page.locator('[data-channel-type="voice"]', { hasText: NAME }).click({ button: 'right' })
    await page.getByRole('menuitem', { name: 'Talk-Einstellungen' }).click()
    await expect(page.getByTestId('talk-settings')).toBeVisible()
  }
  const save = page.getByTestId('unsaved-save')
  const name = page.getByTestId('talk-name')
  const limit = page.getByTestId('preset-limit-input')

  await openSettings()
  await expect(save).toBeDisabled()
  await expect(page.getByTestId('unsaved-status')).toHaveText('')

  // Equivalent values are no change: whitespace in the name, 0 as unlimited.
  await name.fill(`  ${NAME} `)
  await limit.fill('0')
  await expect(save).toBeDisabled()

  // The lobby switch is part of the draft: toggling stores nothing.
  const lobby = page.getByTestId('talk-lobby')
  if (await lobby.count()) {
    await lobby.check()
    await expect(save).toBeEnabled()
    await expect(page.getByTestId('unsaved-status')).toHaveText('Ungespeicherte Änderungen')
    expect((await storedTalk(page, id)).is_lobby).not.toBe(true)
    await lobby.uncheck()
    await expect(save).toBeDisabled()
  }

  // Already open to everyone, a new limit alone can be saved.
  await expect(page.getByTestId('preset-everyone')).toBeChecked()
  await limit.fill('5')
  await expect(save).toBeEnabled()
  expect((await storedTalk(page, id)).user_limit ?? 0).toBe(0)

  // Reset puts the saved state back.
  await page.getByTestId('unsaved-reset').click()
  await expect(limit).toHaveValue('0')
  await expect(save).toBeDisabled()

  // "Only these roles" needs a role before it can be saved.
  await page.getByTestId('preset-roles').check()
  await expect(save).toBeDisabled()
  await expect(page.getByTestId('preset-roles-error')).toBeVisible()
  await page.getByTestId('preset-everyone').check()
  await expect(save).toBeDisabled()

  // Save stores everything at once, and the draft is the saved state afterwards.
  await name.fill(`${NAME} renamed`)
  await limit.fill('5')
  await save.click()
  await expect(save).toBeDisabled()
  await expect(page.getByTestId('unsaved-status')).toHaveText('')
  const stored = await storedTalk(page, id)
  expect(stored.name).toBe(`${NAME} renamed`)
  expect(stored.user_limit).toBe(5)

  // Closing with unsaved changes asks; "keep editing" stays, "discard" drops them.
  await name.fill('Something else')
  await page.keyboard.press('Escape')
  const ask = page.getByRole('alertdialog')
  await expect(ask).toContainText('Änderungen verwerfen?')
  await ask.getByRole('button', { name: 'Weiter bearbeiten' }).click()
  await expect(ask).toBeHidden()
  await expect(name).toHaveValue('Something else')
  await page.keyboard.press('Escape')
  await page.getByRole('alertdialog').getByRole('button', { name: 'Verwerfen' }).click()
  await expect(page.getByTestId('talk-settings')).toHaveCount(0)
  expect((await storedTalk(page, id)).name).toBe(`${NAME} renamed`)

  await apiFetch(page, 'DELETE', `/api/admin/channels/${id}`)
  await context.close()
})
