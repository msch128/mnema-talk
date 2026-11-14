import { test, expect } from './fixtures'
import type { Page } from '@playwright/test'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'cool_guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const SLUG = `clan-${Date.now().toString(36)}`

async function signIn(page: Page, username: string, password: string) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByTestId('login-heading')).toBeVisible()
  await dialog.getByLabel('Benutzername').fill(username)
  await dialog.getByLabel('Passwort').fill(password)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

test('vanity invite link: Owner activates it for sign-ups and switches it off to make it inactive', async ({ browser }) => {
  const adminCtx = await browser.newContext({ viewport: { width: 1280, height: 800 } })
  const visitorCtx = await browser.newContext({ viewport: { width: 1280, height: 800 } })
  const admin = await adminCtx.newPage()
  const visitor = await visitorCtx.newPage()
  try {
    await signIn(admin, ADMIN_USER, ADMIN_PASSWORD)
    await admin.goto('/admin/invites')
    await expect(admin.getByTestId('vanity-link')).toBeVisible()

    // Sets a link name with the switch on (switch starts ON once a name is typed).
    await admin.getByTestId('vanity-slug').fill(SLUG)
    const activeSwitch = admin.getByTestId('vanity-active')
    await expect(activeSwitch).toHaveAttribute('aria-checked', 'true')
    await admin.getByTestId('vanity-save').click()
    await expect(admin.getByTestId('vanity-url')).toContainText(`/join/${SLUG}`)

    // A fresh visitor opens /join/<name> and sees the sign-up form.
    await visitor.goto(`/join/${SLUG}`)
    const dialog = visitor.getByRole('dialog')
    await expect(dialog.getByRole('button', { name: 'Registrieren' })).toBeVisible()
    await expect(dialog.locator('#login-username')).toBeVisible()
    await expect(dialog.locator('#login-invite')).toHaveCount(0)

    // The Owner switches it off.
    await activeSwitch.click()
    await expect(activeSwitch).toHaveAttribute('aria-checked', 'false')
    await admin.getByTestId('vanity-save').click()

    // The same visitor reloads and sees "Dieser Link ist nicht aktiv".
    await visitor.reload()
    await expect(visitor.getByTestId('invite-inactive')).toBeVisible()
    await expect(visitor.getByTestId('invite-inactive-title')).toHaveText('Dieser Link ist nicht aktiv')
  } finally {
    await adminCtx.close()
    await visitorCtx.close()
  }
})
