import AxeBuilder from '@axe-core/playwright'
import type { Page } from '@playwright/test'
import { apiFetch, responseString } from './helpers'
import { test, expect } from './fixtures'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'cool_guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const CHANNEL = `a11y-${Date.now().toString(36)}`

// Every axe rule tagged WCAG 2.0/2.1/2.2 A or AA, with nothing excluded.
const TAGS = ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa']

async function expectNoViolations(page: Page, where: string) {
  await page.waitForTimeout(400) // entry animations finished: axe reads the settled colours
  const result = await new AxeBuilder({ page }).withTags(TAGS).analyze()
  const found = result.violations.map(v => `${v.id}: ${v.nodes.slice(0, 3).map(n => n.target.join(" ") + " " + n.html.slice(0, 140)).join(' | ')}`)
  expect(found, `accessibility violations on ${where}`).toEqual([])
}

async function signIn(page: Page) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByTestId('login-heading')).toBeVisible()
  await expectNoViolations(page, 'the sign-in dialog')
  await dialog.getByLabel('Benutzername').fill(ADMIN_USER)
  await dialog.getByLabel('Passwort').fill(ADMIN_PASSWORD)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

test('the main surfaces meet WCAG AA: chat, menus, settings, admin', async ({ page }) => {
  await signIn(page)
  const channel = await apiFetch(page, 'POST', '/api/admin/channels', { name: CHANNEL, type: 'text' })
  expect(channel.status).toBe(201)
  const id = responseString(channel, 'id')
  expect((await apiFetch(page, 'POST', '/api/admin/channels', { name: `${CHANNEL}-talk`, type: 'voice' })).status).toBe(201)
  expect((await apiFetch(page, 'POST', `/api/channels/${id}/messages`, { content: 'Hello **world** with `code`' })).status).toBe(201)
  await page.goto(`/c/${id}`)
  await expect(page.getByRole('article').filter({ hasText: 'Hello' })).toBeVisible()
  await expectNoViolations(page, 'the chat')

  await page.getByRole('article').filter({ hasText: 'Hello' }).click({ button: 'right' })
  await expect(page.getByRole('menu')).toBeVisible()
  await expectNoViolations(page, 'the message menu')
  await page.keyboard.press('Escape')

  await page.getByRole('button', { name: /Konto-Menü|Konto/ }).last().click()
  await page.getByRole('menuitem', { name: 'Einstellungen' }).click()
  await expect(page.getByRole('dialog', { name: 'Einstellungen' })).toBeVisible()
  await expectNoViolations(page, 'the settings (sign-in)')
  for (const tab of ['sessions', 'privacy', 'data', 'audio']) {
    await page.getByTestId(`settings-tab-${tab}`).click()
    await expect(page.getByTestId(`settings-tab-${tab}`)).toHaveAttribute('aria-selected', 'true')
    await expectNoViolations(page, `the settings (${tab})`)
  }
  await page.keyboard.press('Escape')

  for (const tab of ['users', 'roles', 'channels', 'invites']) {
    await page.goto(`/admin/${tab}`)
    await expect(page.getByTestId('admin-tabs')).toBeVisible()
    await expectNoViolations(page, `admin ${tab}`)
  }
})
