import type { Page } from '@playwright/test'
import { test, expect } from './fixtures'
import { apiFetch, leaveTalks, responseString } from './helpers'

// Where a link or a notification lands: the settings dialog on the section the
// address names (and Back closes it), a notice that jumps to the field that
// fixes it, and the reminder of an event opening that event.

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'cool_guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)

async function signIn(page: Page) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByTestId('login-heading')).toBeVisible()
  await dialog.getByLabel('Benutzername').fill(ADMIN_USER)
  await dialog.getByLabel('Passwort').fill(ADMIN_PASSWORD)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

test('the settings dialog has an address per section, and Back closes it', async ({ browser }) => {
  const ctx = await browser.newContext()
  const page = await ctx.newPage()
  try {
    await signIn(page)
    await expect(page).toHaveURL(/\/c\//)
    const channelUrl = page.url()

    await page.getByTestId('account-menu-button').click()
    await page.getByTestId('menu-settings').click()
    await expect(page.getByRole('dialog', { name: 'Einstellungen' })).toBeVisible()
    // A first visit opens on this device's sound settings; the sign-in section is one click away.
    await expect(page).toHaveURL(/\/settings\/audio$/)

    await page.getByTestId('settings-tab-signin').click()
    await expect(page).toHaveURL(/\/settings\/signin$/)

    await page.goBack()
    await expect(page.getByRole('dialog', { name: 'Einstellungen' })).toBeHidden()
    await expect(page).toHaveURL(channelUrl)

    // A link straight to a section opens it, and closing it leaves for the page underneath.
    await page.goto('/settings/audio')
    await expect(page.getByTestId('settings-tab-audio')).toHaveAttribute('aria-selected', 'true')
    await page.keyboard.press('Escape')
    await expect(page.getByRole('dialog', { name: 'Einstellungen' })).toBeHidden()
    await expect(page).not.toHaveURL(/\/settings\//)
  } finally {
    await ctx.close()
  }
})

test('a notice about the microphone jumps to the input device in the audio settings', async ({ browser }) => {
  const ctx = await browser.newContext({ permissions: [] })
  await ctx.addInitScript(() => {
    // The browser refuses the microphone: joining a Talk says so and offers the fix.
    if (!navigator.mediaDevices) return
    Object.defineProperty(navigator.mediaDevices, 'getUserMedia', {
      configurable: true,
      value: () => Promise.reject(new DOMException('denied', 'NotAllowedError')),
    })
  })
  const page = await ctx.newPage()
  try {
    await signIn(page)
    const talk = await apiFetch(page, 'POST', '/api/admin/channels', { name: `talk-links-${suffix}`, type: 'voice' })
    expect(talk.status).toBe(201)
    await page.reload()
    await page.locator('[data-channel-type="voice"]', { hasText: `talk-links-${suffix}` }).click()
    await page.getByRole('button', { name: 'Audioeinstellungen öffnen' }).click()
    await expect(page.getByTestId('settings-tab-audio')).toHaveAttribute('aria-selected', 'true')
    await expect(page.locator('#audio-input-device')).toBeFocused()
    await page.keyboard.press('Escape')
    await leaveTalks([page])
  } finally {
    await ctx.close()
  }
})

test('the link of an event reminder opens that event', async ({ browser }) => {
  const ctx = await browser.newContext()
  const page = await ctx.newPage()
  try {
    await signIn(page)
    const title = `Reminder night ${suffix}`
    const created = await apiFetch(page, 'POST', '/api/events', { title, description: 'Bring snacks', start_time: new Date(Date.now() + 86_400_000).toISOString(), location: 'Room 42' })
    expect(created.status).toBe(201)
    const id = responseString(created, 'id')
    // What a click on the reminder opens when the app was closed.
    await page.goto(`/?event=${id}`)
    const detail = page.getByRole('dialog')
    await expect(detail.getByRole('heading', { name: title })).toBeVisible()
    await expect(detail.getByRole('button', { name: 'Ich komme' })).toBeVisible()
    await expect(page).not.toHaveURL(/event=/)
  } finally {
    await ctx.close()
  }
})
