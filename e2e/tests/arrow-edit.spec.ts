import { test, expect } from './fixtures'
import type { Page } from '@playwright/test'
import { apiFetch, responseString } from './helpers'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'cool_guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const NAME = `pfeil-${Date.now().toString(36)}`

async function signIn(page: Page) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByTestId('login-heading')).toBeVisible()
  await dialog.getByLabel('Benutzername').fill(ADMIN_USER)
  await dialog.getByLabel('Passwort').fill(ADMIN_PASSWORD)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

test('ArrowUp in the empty composer edits the own messages, walks back and forth, and saves', async ({ browser }) => {
  const context = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  const page = await context.newPage()
  let channel = ''
  try {
    await signIn(page)
    channel = responseString(await apiFetch(page, 'POST', '/api/admin/channels', { name: NAME, type: 'text' }), 'id')
    await page.goto(`/c/${channel}`)
    const composer = page.getByRole('textbox', { name: `Nachricht an #${NAME}` })
    for (const text of ['erste', 'zweite']) {
      await composer.fill(text)
      await composer.press('Enter')
      await expect(page.getByText(text, { exact: true })).toBeVisible()
    }

    // A composer with text keeps ArrowUp for itself.
    await composer.fill('halb')
    await composer.press('ArrowUp')
    await expect(page.getByTestId('message-editor')).toHaveCount(0)
    await composer.fill('')

    // Empty: the newest own message opens, caret at the end.
    await composer.press('ArrowUp')
    const editor = page.getByTestId('message-editor')
    await expect(editor).toHaveValue('zweite')
    await expect(editor).toBeFocused()

    // Older, then newer again, then past the newest back to the composer.
    await editor.press('ArrowUp')
    await expect(editor).toHaveValue('erste')
    await editor.press('ArrowDown')
    await expect(editor).toHaveValue('zweite')
    await editor.press('ArrowDown')
    await expect(editor).toHaveCount(0)
    await expect(composer).toBeFocused()

    // Escape cancels and hands the focus back; Enter saves.
    await composer.press('ArrowUp')
    await editor.press('Escape')
    await expect(editor).toHaveCount(0)
    await expect(composer).toBeFocused()
    await composer.press('ArrowUp')
    await editor.fill('zweite, korrigiert')
    await editor.press('Enter')
    await expect(page.getByText('zweite, korrigiert', { exact: true })).toBeVisible()
    await expect(composer).toBeFocused()
  } finally {
    if (channel) {
      await apiFetch(page, 'POST', '/api/auth/login', { username: ADMIN_USER, password: ADMIN_PASSWORD })
      await apiFetch(page, 'DELETE', `/api/admin/channels/${channel}`)
    }
    await context.close()
  }
})
