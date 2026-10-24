import { test, expect } from './fixtures'
import type { Page } from '@playwright/test'
import { apiFetch, responseString } from './helpers'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'Herzog'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
const FIRST = `entwurf-a-${suffix}`
const SECOND = `entwurf-b-${suffix}`

async function signIn(page: Page) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByRole('heading', { name: 'Mnema Talk' })).toBeVisible()
  await dialog.getByLabel('Benutzername').fill(ADMIN_USER)
  await dialog.getByLabel('Passwort').fill(ADMIN_PASSWORD)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

const draftKeys = (page: Page) => page.evaluate(() => Object.keys(localStorage).filter(key => key.startsWith('mnema.drafts.')))

test('an unsent message waits in its channel across switching and reload, and leaves with sign-out', async ({ browser }) => {
  const context = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  const page = await context.newPage()
  let first = ''
  let second = ''
  try {
    await signIn(page)
    first = responseString(await apiFetch(page, 'POST', '/api/admin/channels', { name: FIRST, type: 'text' }), 'id')
    second = responseString(await apiFetch(page, 'POST', '/api/admin/channels', { name: SECOND, type: 'text' }), 'id')

    await page.goto(`/c/${first}`)
    const composer = page.getByRole('textbox', { name: `Nachricht an #${FIRST}` })
    await composer.fill('not finished yet')

    // Another channel starts empty and marks the first as having a draft.
    await page.locator(`[data-channel-item="${second}"]`).click()
    await expect(page.getByRole('textbox', { name: `Nachricht an #${SECOND}` })).toHaveValue('')
    await expect(page.locator(`[data-channel-item="${first}"]`).getByTestId('channel-draft')).toHaveText('Entwurf')

    // Back again, and after a reload, the text is still there.
    await page.locator(`[data-channel-item="${first}"]`).click()
    await expect(composer).toHaveValue('not finished yet')
    await page.locator(`[data-channel-item="${second}"]`).click()
    await page.reload()
    await page.locator(`[data-channel-item="${first}"]`).click()
    await expect(page.getByRole('textbox', { name: `Nachricht an #${FIRST}` })).toHaveValue('not finished yet')

    // Sending clears it for good.
    await composer.press('Enter')
    await expect(composer).toHaveValue('')
    await page.locator(`[data-channel-item="${second}"]`).click()
    await expect(page.locator(`[data-channel-item="${first}"]`).getByTestId('channel-draft')).toHaveCount(0)

    // Sign-out removes drafts from this browser.
    await page.getByRole('textbox', { name: `Nachricht an #${SECOND}` }).fill('will not stay')
    await expect.poll(() => draftKeys(page)).not.toEqual([])
    await page.getByTestId('account-menu-button').click()
    await page.getByRole('menuitem', { name: 'Abmelden' }).click()
    await expect(page.getByRole('dialog').getByRole('heading', { name: 'Mnema Talk' })).toBeVisible()
    expect(await draftKeys(page)).toEqual([])
  } finally {
    // The channels must not crowd the sidebar for the specs that follow.
    if (first) {
      await apiFetch(page, 'POST', '/api/auth/login', { username: ADMIN_USER, password: ADMIN_PASSWORD })
      await apiFetch(page, 'DELETE', `/api/admin/channels/${first}`)
      await apiFetch(page, 'DELETE', `/api/admin/channels/${second}`)
    }
    await context.close()
  }
})
