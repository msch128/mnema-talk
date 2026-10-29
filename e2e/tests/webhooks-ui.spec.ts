import type { Page } from '@playwright/test'
import { test, expect } from './fixtures'
import { request } from '@playwright/test'
import { apiFetch } from './helpers'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'cool_guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
const NAME = `ui-hooks-${suffix}`

async function signIn(page: Page, username: string, password: string) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('Benutzername').fill(username)
  await dialog.getByLabel('Passwort').fill(password)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

test('the owner creates a webhook in the channel menu and a script posts through it', async ({ browser }) => {
  const page = await (await browser.newContext({ viewport: { width: 1400, height: 900 } })).newPage()
  await signIn(page, ADMIN_USER, ADMIN_PASSWORD)
  expect((await apiFetch(page, 'POST', '/api/admin/channels', { name: NAME, type: 'text' })).status).toBe(201)

  const row = page.locator('[data-channel-type="text"]', { hasText: NAME })
  await row.click()
  await row.click({ button: 'right' })
  await page.getByRole('menuitem', { name: 'Webhooks …' }).click()

  const dialog = page.getByRole('dialog', { name: `Webhooks von #${NAME}` })
  await expect(dialog).toBeVisible()
  await dialog.getByLabel('Name').fill('Pipeline')
  await dialog.getByRole('button', { name: 'Erstellen' }).click()
  const url = (await dialog.getByTestId('revealed-url').locator('code').textContent())?.trim() ?? ''
  expect(url).toContain('/api/webhooks/')
  await expect(dialog.getByTestId('incoming-name')).toHaveText('Pipeline')
  await page.keyboard.press('Escape')

  const script = await request.newContext()
  const text = `from the script ${suffix}`
  expect((await script.post(url, { data: { content: text } })).status()).toBe(204)
  await expect(page.getByText(text)).toBeVisible()
  await expect(page.getByTestId('webhook-badge')).toBeVisible()
  await script.dispose()
})
