import { mkdir } from 'node:fs/promises'
import { test, expect } from './fixtures'
import type { Page } from '@playwright/test'
import { apiFetch } from './helpers'

// The admin console's defaults: a week-long invite, channels created from the
// layout page, an unsaved order that asks before it is thrown away, and the
// settings that moved to Admin > System.

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'cool_guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const SHOTS = process.env.UI_SHOTS_DIR
const CHANNEL = `neu-${Date.now().toString(36)}`

async function signIn(page: Page) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByTestId('login-heading')).toBeVisible()
  await dialog.getByLabel('Benutzername').fill(ADMIN_USER)
  await dialog.getByLabel('Passwort').fill(ADMIN_PASSWORD)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

async function shot(page: Page, name: string) {
  if (!SHOTS) return
  await mkdir(SHOTS, { recursive: true })
  await page.screenshot({ path: `${SHOTS}/${name}.png` })
}

test('invites start at a week, channels are created from the layout page and the settings are where they belong', async ({ browser }) => {
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 800 } })
  const page = await ctx.newPage()
  let channelId = ''
  try {
    await signIn(page)

    // Invites: seven days, and a warning only while nothing limits the link.
    await page.goto('/admin/invites')
    await expect(page.locator('#invite-days')).toHaveValue('7')
    await expect(page.getByTestId('invite-open-forever')).toHaveCount(0)
    await shot(page, 'after-invites')
    await page.locator('[data-preset="never"]').click()
    await expect(page.getByTestId('invite-open-forever')).toBeVisible()
    await shot(page, 'after-invites-warning')

    // Channels & layout: create a channel inside the first category.
    await page.goto('/admin/channels')
    const category = page.locator('[data-testid^="category-item-"]').first()
    await category.getByTestId('add-channel-category').click()
    const dialog = page.getByRole('dialog').last()
    await expect(dialog.locator('#channel-category')).not.toHaveValue('')
    await shot(page, 'after-create-channel')
    await dialog.locator('#channel-name').fill(CHANNEL)
    await dialog.locator('button[type="submit"]').click()
    await expect(category.getByText(CHANNEL)).toBeVisible()
    const channels = await apiFetch(page, 'GET', '/api/channels')
    channelId = channels.json.categories.flatMap(c => c.channels ?? []).find(c => c.name === CHANNEL)?.id ?? ''
    expect(channelId).not.toBe('')

    // An unsaved order asks before the console closes.
    await category.getByTestId('move-down-category').click()
    await expect(page.getByTestId('save-layout-button')).toBeVisible()
    await page.getByRole('dialog').first().locator('[data-dialog-close]').click()
    const confirm = page.getByRole('alertdialog').or(page.getByRole('dialog').last())
    await expect(confirm.getByRole('button', { name: 'Änderungen verwerfen' })).toBeVisible()
    await shot(page, 'after-discard-layout')
    await confirm.getByRole('button', { name: 'Änderungen verwerfen' }).click()
    await expect(page.locator('[data-testid="save-layout-button"]')).toHaveCount(0)

    // Direct messages and sign-out live on System.
    await page.goto('/admin/access')
    const access = page.getByTestId('access-settings')
    await expect(access).toBeVisible()
    await expect(access.getByTestId('inactivity-days')).toHaveValue('7')
    await shot(page, 'after-system-access')
    await page.goto('/admin/onboarding')
    await expect(page.locator('#onb-name')).toBeVisible()
    await expect(page.getByTestId('inactivity-setting')).toHaveCount(0)
    await shot(page, 'after-welcome-rules')
  } finally {
    if (channelId) await apiFetch(page, 'DELETE', `/api/admin/channels/${channelId}`)
    await ctx.close()
  }
})
