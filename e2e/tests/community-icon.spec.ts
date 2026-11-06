import { test, expect } from './fixtures'
import type { Page } from '@playwright/test'
import { apiFetch } from './helpers'

// The community icon is a field of the Welcome & Rules form like every other one
// (doc/settings-ux.md): picking a picture only previews it, Save stores it.
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

// A plain square PNG drawn by the page itself: no media file in the repository.
async function squarePng(page: Page): Promise<Buffer> {
  const base64 = await page.evaluate(() => {
    const canvas = document.createElement('canvas')
    canvas.width = 160
    canvas.height = 160
    const g = canvas.getContext('2d')!
    g.fillStyle = '#10b981'
    g.fillRect(0, 0, 160, 160)
    return canvas.toDataURL('image/png').split(',')[1] ?? ''
  })
  return Buffer.from(base64, 'base64')
}

const healthIcon = async (page: Page) => ((await apiFetch(page, 'GET', '/api/health')).json as { community_icon?: string }).community_icon ?? ''

test('the community icon is staged with a preview and stored only on Save', async ({ page }) => {
  await signIn(page)
  await page.goto('/admin/onboarding')
  const form = page.getByTestId('onboarding-settings')
  await expect(form).toBeVisible()
  const before = await healthIcon(page)
  const save = form.getByTestId('onboarding-save')
  await expect(save).toBeDisabled()

  await form.getByTestId('community-icon-input').setInputFiles({ name: 'icon.png', mimeType: 'image/png', buffer: await squarePng(page) })
  await expect(form.getByTestId('community-icon-preview')).toHaveAttribute('src', /^blob:/)
  await expect(form.getByTestId('community-icon-pending')).toBeVisible()
  await expect(form.getByTestId('unsaved-status')).not.toBeEmpty()
  await expect(save).toBeEnabled()
  expect(await healthIcon(page)).toBe(before)

  await save.click()
  await expect(page.getByText('Willkommen und Regeln gespeichert.')).toBeVisible()
  await expect(form.getByTestId('community-icon-pending')).toBeHidden()
  await expect(save).toBeDisabled()
  const stored = await healthIcon(page)
  expect(stored).not.toBe('')
  expect(stored).not.toBe(before)
  await expect(form.getByTestId('community-icon-preview')).toHaveAttribute('src', new RegExp(`v=${stored}`))

  // Remove is staged the same way.
  await form.getByTestId('community-icon-remove').click()
  await expect(form.getByTestId('community-icon-preview')).toBeHidden()
  expect(await healthIcon(page)).toBe(stored)
  await save.click()
  await expect(save).toBeDisabled()
  await expect.poll(() => healthIcon(page)).toBe('')
})
