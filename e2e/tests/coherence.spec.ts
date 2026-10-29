import type { Page } from '@playwright/test'
import { test, expect } from './fixtures'
import { apiFetch, responseString } from './helpers'

// Offline banner and keyboard order of the main layout (1.1 coherence items).

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'Admin'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
const CHANNEL = `tastatur-${suffix}`

async function signIn(page: Page) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByTestId('login-heading')).toBeVisible()
  await dialog.getByLabel('Benutzername').fill(ADMIN_USER)
  await dialog.getByLabel('Passwort').fill(ADMIN_PASSWORD)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
  await expect(page.getByTestId('chat-channel-name')).toBeVisible()
}

test('the browser going offline shows the connection banner at once, and it clears when the network is back', async ({ page, context }) => {
  await signIn(page)
  await expect(page.getByTestId('chat-channel-name')).toBeVisible()
  await context.setOffline(true)
  await expect(page.getByTestId('connection-banner')).toBeVisible({ timeout: 5_000 })
  await expect(page.getByTestId('connection-banner')).toContainText('offline')
  await context.setOffline(false)
  await expect(page.getByTestId('connection-banner')).toBeHidden({ timeout: 30_000 })
})

type Region = 'skip' | 'sidebar' | 'header' | 'messages' | 'composer' | 'members' | 'other'

function classify(page: Page): Promise<Region> {
  return page.evaluate((): Region => {
    const el = document.activeElement
    if (!el) return 'other'
    if (el.closest('[data-testid="skip-links"]')) return 'skip'
    if (el.closest('[data-testid="channel-drawer"]')) return 'sidebar'
    if (el.hasAttribute('data-composer-input')) return 'composer'
    if (el.closest('[data-msg-id]')) return 'messages'
    if (el.closest('main header')) return 'header'
    if (el.closest('aside')) return 'members'
    return 'other'
  })
}

test('skip links come first and Tab runs sidebar, header, messages, composer, then the member list', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  await signIn(page)
  const made = await apiFetch(page, 'POST', '/api/admin/channels', { name: CHANNEL, type: 'text' })
  expect(made.status).toBe(201)
  const id = responseString(made, 'id')
  try {
    expect((await apiFetch(page, 'POST', `/api/channels/${id}/messages`, { content: `Tastatur ${suffix}` })).status).toBe(201)
    await page.reload()
    await expect(page.getByTestId('chat-channel-name')).toBeVisible()
    await page.locator('[data-channel-type="text"]', { hasText: CHANNEL }).click()
    await expect(page.locator('[data-msg-id]', { hasText: `Tastatur ${suffix}` })).toBeVisible()

    // A reload puts focus at the top of the document, the way a keyboard user arrives at the page.
    await page.reload()
    await expect(page.getByTestId('chat-channel-name')).toHaveText(CHANNEL)
    await expect(page.locator('[data-msg-id]', { hasText: `Tastatur ${suffix}` })).toBeVisible()
    await page.evaluate(() => {
      // A focusable marker before everything: the next Tab goes to the first stop of the page.
      const marker = document.createElement('span')
      marker.tabIndex = -1
      document.body.prepend(marker)
      marker.focus()
    })
    const seen: Region[] = []
    for (let i = 0; i < 120; i++) {
      await page.keyboard.press('Tab')
      const region = await classify(page)
      if (seen.at(-1) !== region) seen.push(region)
      if (region === 'members') break
    }
    const named = seen.filter(r => r !== 'other')
    expect(named.slice(0, 6)).toEqual(['skip', 'sidebar', 'header', 'messages', 'composer', 'members'])

    // The first skip link lands on the messages, the second in the message box.
    await page.getByTestId('skip-to-main').focus()
    await page.keyboard.press('Enter')
    await expect(page.locator('main#main-content')).toBeFocused()
    await page.getByTestId('skip-to-composer').focus()
    await page.keyboard.press('Enter')
    await expect(page.locator('[data-composer-input]')).toBeFocused()
  } finally {
    await apiFetch(page, 'DELETE', `/api/admin/channels/${id}`)
  }
})
