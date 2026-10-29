import type { Page } from '@playwright/test'
import { test, expect } from './fixtures'
import { apiFetch, responseString } from './helpers'

// The shared fixtures fail every spec on a Content-Security-Policy violation.
// This spec makes sure the places that used to need `style-src 'unsafe-inline'`
// are actually visited under the real policy and still look right: the emoji
// picker (a third-party web component with a shadow root), Markdown with code
// blocks, the admin panels and the API reference.

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'Admin'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
const CHANNEL = `csp-${suffix}`
const TABS = ['users', 'roles', 'channels', 'invites', 'onboarding', 'emoji', 'bans', 'bots', 'media', 'audit', 'system']

async function signIn(page: Page) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByRole('heading', { name: 'Mnema Talk' })).toBeVisible()
  await dialog.getByLabel('Benutzername').fill(ADMIN_USER)
  await dialog.getByLabel('Passwort').fill(ADMIN_PASSWORD)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

test('the policy has no inline style sources', async ({ page }) => {
  const response = await page.goto('/')
  const csp = response?.headers()['content-security-policy'] ?? ''
  expect(csp).toContain("style-src 'self';")
  expect(csp).toContain("style-src-attr 'none'")
  expect(csp).not.toContain('unsafe-inline')
})

test('emoji picker, Markdown, admin panels and API reference run under the policy', async ({ page }) => {
  await signIn(page)
  const made = await apiFetch(page, 'POST', '/api/admin/channels', { name: CHANNEL, type: 'text' })
  expect(made.status).toBe(201)
  const id = responseString(made, 'id')
  try {
    const markdown = '**bold** `code`\n\n```go\nfunc main() { println("hi") }\n```\n\n> quote\n\n- one\n- two'
    expect((await apiFetch(page, 'POST', `/api/channels/${id}/messages`, { content: markdown })).status).toBe(201)
    await page.goto(`/c/${id}`)
    const block = page.locator('pre code').first()
    await expect(block).toBeVisible()
    await expect(block.locator('[class*="hljs-"]').first()).toBeVisible()

    // The picker's stylesheet is adopted (not a <style> element) and takes effect.
    await page.getByTestId('emoji-button').click()
    const picker = page.getByTestId('emoji-picker')
    await expect(picker).toBeVisible()
    const state = await picker.locator('emoji-picker').evaluate((el) => {
      const root = el.shadowRoot
      const nav = root?.querySelector('.nav')
      return {
        adopted: root?.adoptedStyleSheets.length ?? 0,
        styleElements: root?.querySelectorAll('style').length ?? 0,
        navColumns: nav ? getComputedStyle(nav).gridTemplateColumns.split(' ').length : 0,
        groups: root?.querySelector('.picker') ? getComputedStyle(root.querySelector('.picker') as Element).getPropertyValue('--num-groups').trim() : '',
      }
    })
    expect(state.adopted).toBe(2) // the picker's own sheet and the Mnema look (lib/emojiPickerSheet)
    expect(state.styleElements).toBe(0)
    expect(state.navColumns).toBeGreaterThan(1) // grid-template-columns from the style binding
    expect(Number(state.groups)).toBeGreaterThan(1) // --num-groups from the picker's style binding
    const box = await picker.boundingBox()
    expect(box?.height ?? 0).toBeGreaterThan(200)

    // Searching renders emoji buttons; picking one lands in the composer.
    await picker.locator('emoji-picker').locator('input[type="search"]').fill('smile')
    const first = picker.locator('emoji-picker').getByRole('option').first()
    await expect(first).toBeVisible()
    await first.click()
    await expect(page.getByRole('textbox', { name: `Nachricht an #${CHANNEL}` })).not.toHaveValue('')

    for (const tab of TABS) {
      await page.goto(`/admin/${tab}`)
      await expect(page.getByTestId(`tab-${tab}`)).toHaveAttribute('aria-pressed', 'true')
    }

    await page.goto('/api/docs')
    await expect(page.locator('#swagger-ui .swagger-ui').first()).toBeVisible()
  } finally {
    await page.goto('/') // the API reference page does not allow fetches back to the API
    await apiFetch(page, 'DELETE', `/api/admin/channels/${id}`)
  }
})
