import { test, expect } from './fixtures'
import { apiFetch, responseString } from './helpers'

// "Statustext festlegen" in the presence menu opens a small dialog of its own
// (not the profile): text, emoji, "Löschen nach", Save and Clear, by keyboard
// too. Every UI string is German (the suite's locale).

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'cool_guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)

test('a member sets a status message with an expiry from the presence menu, edits it and clears it', async ({ browser }) => {
  const owner = await (await browser.newContext()).newPage()
  await owner.goto('/')
  expect((await apiFetch(owner, 'POST', '/api/auth/login', { username: ADMIN_USER, password: ADMIN_PASSWORD })).status).toBe(200)
  const invite = await apiFetch(owner, 'POST', '/api/admin/invites', { max_uses: 1 })
  const page = await (await browser.newContext({ viewport: { width: 1280, height: 800 } })).newPage()
  await page.goto('/')
  const registered = await apiFetch(page, 'POST', '/api/auth/register', { username: `status${suffix}`, password: `pw-${suffix}-status-x1`, invite_code: responseString(invite, 'code') })
  expect(registered.status).toBe(201)
  await page.goto('/')

  const openStatusDialog = async () => {
    await page.getByTestId('presence-button').click()
    await page.getByRole('menuitem', { name: /Statustext/ }).click()
    await expect(page.getByTestId('status-dialog')).toBeVisible()
  }

  // The entry point opens the status dialog with the text field focused, not the profile.
  await openStatusDialog()
  await expect(page.getByTestId('profile-status')).toHaveCount(0)
  await expect(page.getByTestId('status-input')).toBeFocused()

  // Text, "Löschen nach: 4 Stunden", Enter saves.
  await page.getByTestId('status-input').fill('Mittagspause')
  await page.locator('[data-expiry="hours4"]').click()
  await expect(page.locator('[data-expiry="hours4"]')).toHaveAttribute('aria-checked', 'true')
  await page.getByTestId('status-input').press('Enter')
  await expect(page.getByTestId('status-dialog')).toBeHidden()
  await expect(page.getByTestId('own-subline')).toHaveText('Mittagspause')
  const me = await apiFetch(page, 'GET', '/api/auth/me')
  const until = Date.parse(responseString(me, 'status_expires_at'))
  expect(Math.abs(until - (Date.now() + 4 * 3_600_000))).toBeLessThan(5 * 60_000)

  // Editing starts from the saved text; Escape closes without changes.
  await openStatusDialog()
  await expect(page.getByTestId('status-input')).toHaveValue('Mittagspause')
  await page.getByTestId('status-input').fill('Etwas anderes')
  await page.keyboard.press('Escape')
  await expect(page.getByTestId('status-dialog')).toBeHidden()
  await expect(page.getByTestId('own-subline')).toHaveText('Mittagspause')

  // Clear removes the text and the expiry.
  await openStatusDialog()
  await page.getByTestId('status-clear').click()
  await expect(page.getByTestId('status-dialog')).toBeHidden()
  await expect(page.getByTestId('own-subline')).toHaveText('Online')
  expect((await apiFetch(page, 'GET', '/api/auth/me')).json).toMatchObject({ status_text: '' })
})
