import { test, expect } from './fixtures'
import type { Page } from '@playwright/test'
import { apiFetch, responseString } from './helpers'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'cool_guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
const CHANNEL = `ausgang-${suffix}`

async function signIn(page: Page) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByTestId('login-heading')).toBeVisible()
  await dialog.getByLabel('Benutzername').fill(ADMIN_USER)
  await dialog.getByLabel('Passwort').fill(ADMIN_PASSWORD)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

test('a message written offline waits in the conversation and is sent once when the connection returns', async ({ browser }) => {
  const context = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  const page = await context.newPage()
  try {
    await signIn(page)
    const id = responseString(await apiFetch(page, 'POST', '/api/admin/channels', { name: CHANNEL, type: 'text' }), 'id')
    await page.goto(`/c/${id}`)
    const composer = page.getByRole('textbox', { name: `Nachricht an #${CHANNEL}` })
    await expect(composer).toBeVisible()

    await context.setOffline(true)
    const text = `offline-${suffix}`
    await composer.fill(text)
    await composer.press('Enter')

    // The composer cleared; the message sits in the conversation, marked as not sent yet.
    await expect(composer).toHaveValue('')
    const pending = page.getByTestId('pending-message')
    await expect(pending).toHaveCount(1)
    await expect(pending).toContainText(text)
    await expect(pending).toContainText('Ausstehend')

    await context.setOffline(false)
    await expect(pending).toHaveCount(0, { timeout: 60_000 })
    await expect(page.getByText(text, { exact: true })).toHaveCount(1)
    // Still one message after a reload: it was stored once.
    await page.reload()
    await expect(page.getByText(text, { exact: true })).toHaveCount(1)
  } finally {
    await context.close()
  }
})

test('a pending message can be discarded before it is ever sent', async ({ browser }) => {
  const context = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  const page = await context.newPage()
  try {
    await signIn(page)
    const id = responseString(await apiFetch(page, 'POST', '/api/admin/channels', { name: `${CHANNEL}-b`, type: 'text' }), 'id')
    await page.goto(`/c/${id}`)
    const composer = page.getByRole('textbox', { name: `Nachricht an #${CHANNEL}-b` })
    await context.setOffline(true)
    await composer.fill('never sent')
    await composer.press('Enter')
    const pending = page.getByTestId('pending-message')
    await expect(pending).toHaveCount(1)
    await pending.getByRole('button', { name: 'Verwerfen' }).click()
    await expect(pending).toHaveCount(0)
    await context.setOffline(false)
    await page.waitForTimeout(8000)
    await expect(page.getByText('never sent', { exact: true })).toHaveCount(0)
  } finally {
    await context.close()
  }
})

test('a message written offline survives a reload and is sent when the member is back', async ({ browser }) => {
  const context = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  const page = await context.newPage()
  try {
    await signIn(page)
    const id = responseString(await apiFetch(page, 'POST', '/api/admin/channels', { name: `${CHANNEL}-c`, type: 'text' }), 'id')
    await page.goto(`/c/${id}`)
    const composer = page.getByRole('textbox', { name: `Nachricht an #${CHANNEL}-c` })
    await expect(composer).toBeVisible()
    await context.setOffline(true)
    await composer.fill('kept over a reload')
    await composer.press('Enter')
    await expect(page.getByTestId('pending-message')).toHaveCount(1)
    await context.setOffline(false)
    await page.reload()
    await expect(page.getByText('kept over a reload', { exact: true })).toHaveCount(1, { timeout: 60_000 })
    await expect(page.getByTestId('pending-message')).toHaveCount(0, { timeout: 60_000 })
  } finally {
    await context.close()
  }
})
