import { test, expect } from './fixtures'
import type { Page } from '@playwright/test'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'cool_guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

async function signIn(page: Page) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByTestId('login-heading')).toBeVisible()
  await dialog.getByLabel('Benutzername').fill(ADMIN_USER)
  await dialog.getByLabel('Passwort').fill(ADMIN_PASSWORD)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

test('push to talk starts without a key, asks for one, and shows what it can and cannot do', async ({ browser }) => {
  const context = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  const page = await context.newPage()
  try {
    await signIn(page)
    await page.getByTestId('account-menu-button').click()
    // The account menu has its own door to the microphone check.
    await page.getByTestId('menu-sound').click()
    const dialog = page.getByTestId('settings-dialog')
    await expect(dialog.getByTestId('mic-test-toggle')).toBeVisible()
    await expect(dialog.getByTestId('settings-tab-audio')).toHaveAttribute('aria-selected', 'true')

    // Choosing push to talk asks for a key at once: Space is no longer taken for granted.
    await dialog.getByRole('button', { name: /Push-to-Talk/ }).first().click()
    const keyButton = dialog.getByTestId('ptt-key-button')
    await expect(keyButton).toHaveText('Taste drücken …')
    await page.keyboard.press('F8')
    await expect(keyButton).toHaveText('F8')

    // The hint says where the key works, and the line under it names the key.
    await expect(dialog.getByTestId('ptt-key-panel')).toContainText('Textfeld')
    await expect(dialog.getByTestId('ptt-key-panel')).toContainText('Desktop-App')
    const state = dialog.getByTestId('ptt-key-state')
    await expect(state).toContainText('F8')

    // The next time the dialog opens, it is where the member left it.
    await page.keyboard.press('Escape')
    await expect(dialog).toBeHidden()
    await page.getByTestId('account-menu-button').click()
    await page.getByTestId('menu-settings').click()
    await expect(page.getByTestId('settings-tab-audio')).toHaveAttribute('aria-selected', 'true')
  } finally {
    await context.close()
  }
})
