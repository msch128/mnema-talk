import type { Page } from '@playwright/test'
import { test, expect } from './fixtures'
import { apiFetch, responseString } from './helpers'

// Own account security in the settings: the password change refuses a
// mismatch and a wrong current password and ends the other sessions, a second
// device shows in the session list and is signed out from there, signing out
// leads back to the sign-in form, and afterwards only the new password works. Every UI string is German
// (the suite's locale).

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'cool_guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
const MEMBER = `secure_${suffix}`
const OLD_PASSWORD = `old-${suffix}-password-1`
const NEW_PASSWORD = `new-${suffix}-password-2`
const VIEWPORT = { width: 1400, height: 900 }

async function signIn(page: Page, username: string, password: string) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByTestId('login-heading')).toBeVisible()
  await dialog.getByLabel('Benutzername').fill(username)
  await dialog.getByLabel('Passwort').fill(password)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
}

async function openSignInSettings(page: Page) {
  await page.getByTestId('account-menu-button').click()
  await page.getByTestId('menu-settings').click()
  await page.getByTestId('settings-tab-signin').click()
}

test('a member changes the password, which ends other sessions, signs a second device out from the list, and only the new password signs in again', async ({ browser }) => {
  const ownerCtx = await browser.newContext({ viewport: VIEWPORT })
  const phoneCtx = await browser.newContext({ viewport: VIEWPORT })
  const laptopCtx = await browser.newContext({ viewport: VIEWPORT })
  const owner = await ownerCtx.newPage()
  const laptop = await laptopCtx.newPage()
  const second = await phoneCtx.newPage()
  try {
    await signIn(owner, ADMIN_USER, ADMIN_PASSWORD)
    await expect(owner.getByTestId('account-menu-button')).toBeVisible()
    const invite = await apiFetch(owner, 'POST', '/api/admin/invites', { max_uses: 1 })
    await laptop.goto('/')
    expect((await apiFetch(laptop, 'POST', '/api/auth/register', { username: MEMBER, password: OLD_PASSWORD, invite_code: responseString(invite, 'code') })).status).toBe(201)
    await laptop.reload()
    await signIn(second, MEMBER, OLD_PASSWORD)
    await expect(second.getByTestId('account-menu-button')).toBeVisible()

    // The password form refuses a mismatch and a wrong current password, and says why.
    await openSignInSettings(laptop)
    const card = laptop.getByTestId('account-password')
    await card.getByRole('button', { name: 'Ändern' }).click()
    await card.getByLabel('Aktuelles Passwort').fill(OLD_PASSWORD)
    await card.getByLabel(/^Neues Passwort \(/).fill(NEW_PASSWORD)
    await card.getByLabel('Neues Passwort wiederholen').fill(`${NEW_PASSWORD}-typo`)
    await card.getByRole('button', { name: 'Passwort ändern' }).click()
    await expect(card.getByRole('alert')).toHaveText('Die beiden Passwörter stimmen nicht überein.')
    await card.getByLabel('Aktuelles Passwort').fill(`${OLD_PASSWORD}-wrong`)
    await card.getByLabel('Neues Passwort wiederholen').fill(NEW_PASSWORD)
    await card.getByRole('button', { name: 'Passwort ändern' }).click()
    await expect(card.getByRole('alert')).toBeVisible()
    await expect(card.getByLabel('Neues Passwort wiederholen')).toBeVisible()

    // The right current password changes it.
    await card.getByLabel('Aktuelles Passwort').fill(OLD_PASSWORD)
    await card.getByRole('button', { name: 'Passwort ändern' }).click()
    await expect(laptop.getByText('Passwort geändert')).toBeVisible()
    await expect(card.getByLabel('Aktuelles Passwort')).toHaveCount(0)

    // Changing the password ends every other session: the second device is refused and the list shows only this one.
    await expect.poll(async () => (await apiFetch(second, 'GET', '/api/auth/me')).status).toBe(401)
    await laptop.getByTestId('settings-tab-sessions').click()
    const sessions = laptop.getByTestId('account-sessions')
    await expect(sessions.getByTestId('session-current')).toBeVisible()
    await expect(sessions.getByTestId('sessions-only-this')).toBeVisible()

    // The second device signs in with the new password; the list now offers to end it from here.
    await signIn(second, MEMBER, NEW_PASSWORD)
    await expect(second.getByTestId('account-menu-button')).toBeVisible()
    await laptop.getByTestId('settings-tab-signin').click()
    await laptop.getByTestId('settings-tab-sessions').click()
    await expect(sessions.getByTestId('session-sign-out')).toHaveCount(2)
    await sessions.getByTestId('sessions-sign-out-others').click()
    await laptop.getByRole('alertdialog').getByRole('button', { name: 'Alle anderen Sitzungen abmelden' }).click()
    await expect(sessions.getByTestId('sessions-only-this')).toBeVisible()
    await expect.poll(async () => (await apiFetch(second, 'GET', '/api/auth/me')).status).toBe(401)

    // Signing out leads back to the form; the old password no longer works, the new one does.
    await laptop.keyboard.press('Escape')
    await laptop.getByTestId('account-menu-button').click()
    await laptop.getByRole('menuitem', { name: 'Abmelden' }).click()
    const dialog = laptop.getByRole('dialog')
    await expect(dialog.getByTestId('login-heading')).toBeVisible()
    await dialog.getByLabel('Benutzername').fill(MEMBER)
    await dialog.getByLabel('Passwort').fill(OLD_PASSWORD)
    await dialog.getByRole('button', { name: 'Anmelden' }).click()
    await expect(dialog.getByRole('alert')).toBeVisible()
    await expect(laptop.getByTestId('account-menu-button')).toHaveCount(0)
    await dialog.getByLabel('Passwort').fill(NEW_PASSWORD)
    await dialog.getByRole('button', { name: 'Anmelden' }).click()
    await expect(dialog).toBeHidden()
    await expect(laptop.getByTestId('account-menu-button')).toBeVisible()
  } finally {
    await ownerCtx.close()
    await phoneCtx.close()
    await laptopCtx.close()
  }
})
