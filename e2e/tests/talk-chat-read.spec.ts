import type { Page } from '@playwright/test'
import { apiFetch, responseString, leaveTalks } from './helpers'
import { test, expect } from './fixtures'

// A Talk's chat can be read and written from the Talk's row without joining
// the Talk: no microphone request, not in the participant list, until Join.

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'cool_guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
const CHANNEL = `Lesesaal ${suffix}`

async function signIn(page: Page) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('Benutzername').fill(ADMIN_USER)
  await dialog.getByLabel('Passwort').fill(ADMIN_PASSWORD)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

const mediaRequests = (page: Page) => page.evaluate(() => (window as unknown as { __media: number }).__media)

test('read and write a Talk chat from its row, then join from the header', async ({ browser }) => {
  const ctx = await browser.newContext({ permissions: ['microphone', 'camera'], viewport: { width: 1400, height: 900 } })
  // Counts every request for the microphone or camera.
  await ctx.addInitScript(() => {
    const w = window as unknown as { __media: number }
    w.__media = 0
    const devices = navigator.mediaDevices
    if (!devices) return
    const original = devices.getUserMedia.bind(devices)
    devices.getUserMedia = (constraints) => {
      w.__media++
      return original(constraints)
    }
  })
  const page = await ctx.newPage()
  await signIn(page)
  const created = await apiFetch(page, 'POST', '/api/admin/channels', { name: CHANNEL, type: 'voice' })
  expect(created.status).toBe(201)
  const channelSlug = responseString(created, 'slug')
  await page.reload()

  const row = page.locator('[data-channel-type="voice"]', { hasText: CHANNEL })
  await row.hover()
  // The chat button sits beside the row (a button inside a button is not valid markup).
  const chatButton = page.locator('[data-channel-item]', { has: row }).getByRole('button', { name: 'Chat öffnen' })
  await expect(chatButton).toBeVisible()
  await chatButton.click()

  // The Talk is shown as a preview with its chat; nothing joined.
  await expect(page).toHaveURL(new RegExp(`/v/${channelSlug}/chat$`))
  const panel = page.getByTestId('voice-chat-panel')
  await expect(panel).toBeVisible()
  await expect(page.getByTestId('talk-header-join')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Verlassen' })).toHaveCount(0)
  await expect(page.locator('[data-voice-user]')).toHaveCount(0)
  expect(await mediaRequests(page)).toBe(0)

  const text = `Nur mitgelesen ${suffix}`
  const composer = panel.getByRole('textbox', { name: `Nachricht an ${CHANNEL}` })
  await composer.fill(text)
  await composer.press('Enter')
  await expect(panel.getByText(text)).toBeVisible()
  await expect(page.locator('[data-voice-user]')).toHaveCount(0)
  expect(await mediaRequests(page)).toBe(0)

  // Join from the header.
  await page.getByTestId('talk-header-join').click()
  await expect(page.getByRole('button', { name: 'Verlassen' }).first()).toBeVisible()
  await expect(page.getByTestId('talk-header-join')).toHaveCount(0)
  await leaveTalks([page])
  await ctx.close()
})
