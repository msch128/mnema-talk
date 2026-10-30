import { mkdir } from 'node:fs/promises'
import { test, expect, showFirstRun } from './fixtures'
import type { Page } from '@playwright/test'
import { apiFetch } from './helpers'

// The Owner's first run: a calm "Get started" list under the channel header.
// Its steps tick from real state, it can be closed and the server menu brings
// it back. The suite shares one Owner and one community, so the spec controls
// what it can (the community name, whether the list is closed) and asserts on
// those steps only.

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'cool_guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const SHOTS = process.env.UI_SHOTS_DIR
const COMMUNITY = `Heimat ${Date.now().toString(36)}`

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

async function setName(page: Page, name: string) {
  const current = await apiFetch(page, 'GET', '/api/admin/onboarding')
  const s = current.json as { welcome_text: string; rules_text: string; start_channel_id: string | null }
  expect((await apiFetch(page, 'PUT', '/api/admin/onboarding', { community_name: name, welcome_text: s.welcome_text, rules_text: s.rules_text, start_channel_id: s.start_channel_id, require_reacceptance: false })).status).toBe(200)
}

test('a fresh community walks its Owner through naming it, inviting people and finding the list again', async ({ browser }) => {
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 800 } })
  const page = await ctx.newPage()
  try {
    await signIn(page)
    // A first run: unnamed community, checklist open.
    await setName(page, '')
    expect((await apiFetch(page, 'PUT', '/api/admin/first-run', { hidden: false })).status).toBe(200)
    await showFirstRun(ctx)
    await page.goto('/')

    const card = page.getByTestId('first-run')
    await expect(card).toBeVisible()
    await expect(card.locator('[data-step="name"]')).toHaveAttribute('data-done', 'false')
    await shot(page, 'after-01-get-started')

    // Name it where the step points: Welcome & rules.
    await card.getByTestId('first-run-name').click()
    const admin = page.getByRole('dialog')
    await expect(admin.locator('#onb-name')).toBeVisible()
    await shot(page, 'after-02-welcome-rules')
    await admin.locator('#onb-name').fill(COMMUNITY)
    await admin.getByTestId('onboarding-save').click()
    await expect(admin.locator('#onb-name')).toHaveValue(COMMUNITY)
    await admin.locator('[data-dialog-close]').click()
    await expect(admin).toBeHidden()

    // The step ticks itself and the title carries the new name.
    await expect(card.locator('[data-step="name"]')).toHaveAttribute('data-done', 'true')
    await expect(card.getByRole('heading', { level: 2 })).toContainText(COMMUNITY)

    await shot(page, 'after-03-named')

    // The invite shortcut makes a link and ticks its step.
    await page.context().grantPermissions(['clipboard-read', 'clipboard-write']).catch(() => undefined)
    if (await card.getByTestId('first-run-invite').count()) {
      await card.getByTestId('first-run-invite').click()
      await expect(card.locator('[data-step="invite"]')).toHaveAttribute('data-done', 'true')
    }

    await shot(page, 'after-04-invited')

    // Closing the list keeps it one click away in the server menu.
    await card.getByTestId('first-run-hide').click()
    await expect(card).toBeHidden()
    await page.reload()
    await expect(page.locator('#main-content')).toBeVisible()
    await expect(page.getByTestId('first-run')).toHaveCount(0)
    await page.getByRole('button', { name: COMMUNITY }).first().click()
    await page.getByTestId('menu-first-run').click()
    await expect(page.getByTestId('first-run')).toBeVisible()
  } finally {
    await setName(page, '')
    await ctx.close()
  }
})
