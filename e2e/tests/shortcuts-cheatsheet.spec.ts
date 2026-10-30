import { test, expect } from './fixtures'
import type { Page } from '@playwright/test'
import { apiFetch, responseString } from './helpers'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'cool_guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
const CHANNEL = `keys-${suffix}`

async function signIn(page: Page) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByTestId('login-heading')).toBeVisible()
  await dialog.getByLabel('Benutzername').fill(ADMIN_USER)
  await dialog.getByLabel('Passwort').fill(ADMIN_PASSWORD)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

test('the shortcut list opens with Ctrl+/ and ?, from the account menu, and not while typing; a thread is one click away', async ({ browser }) => {
  const context = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  const page = await context.newPage()
  let channelId = ''
  try {
    await signIn(page)
    const channel = await apiFetch(page, 'POST', '/api/admin/channels', { name: CHANNEL, type: 'text' })
    expect(channel.status).toBe(201)
    channelId = responseString(channel, 'id')
    await page.goto(`/c/${channelId}`)
    const composer = page.getByRole('textbox', { name: `Nachricht an #${CHANNEL}` })
    await expect(composer).toBeVisible()

    // A question mark typed into the composer is only text.
    await composer.fill('')
    await composer.press('Shift+?')
    await expect(page.getByTestId('shortcuts-dialog')).toBeHidden()
    await expect(composer).toHaveValue('?')
    await composer.fill('')

    // Ctrl+/ from anywhere, even from the composer, and again to close.
    await page.keyboard.press('Control+/')
    const dialog = page.getByTestId('shortcuts-dialog')
    await expect(dialog).toBeVisible()
    await expect(dialog.locator('[data-shortcut="search"] kbd')).toHaveText([/^(Ctrl|⌘)$/, 'K'])
    await expect(dialog.locator('[data-shortcut="reply"] kbd')).toHaveText(['R'])
    await page.keyboard.press('Control+/')
    await expect(dialog).toBeHidden()

    // "?" outside a field, and Escape closes.
    await page.locator('body').click({ position: { x: 700, y: 120 } })
    await page.keyboard.press('Shift+?')
    await expect(dialog).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(dialog).toBeHidden()

    // The account menu lists it too.
    await page.getByTestId('account-menu-button').click()
    await page.getByTestId('menu-shortcuts').click()
    await expect(dialog).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(dialog).toBeHidden()

    // A message has the thread one click away, named by what it does.
    await composer.fill('thread please')
    await composer.press('Enter')
    const message = page.locator('[data-msg-id]', { hasText: 'thread please' })
    await message.hover()
    await expect(message.getByTestId('msg-open-thread')).toHaveAccessibleName('Im Thread antworten')
    await message.getByTestId('msg-open-thread').click()
    await expect(page.getByTestId('thread-hint')).toHaveText('Antworten hier landen nicht im Hauptchat.')
  } finally {
    if (channelId) await apiFetch(page, 'DELETE', `/api/admin/channels/${channelId}`).catch(() => undefined)
    await context.close()
  }
})
