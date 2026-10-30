import type { Page } from '@playwright/test'
import { apiFetch, trackPeerConnections, leaveTalks, expectTalkLive } from './helpers'
import { test, expect } from './fixtures'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'cool_guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)

declare global {
  interface Window { __audioRequests: string[] }
}

// Every microphone the page asks the browser for, by the device it names.
const recordMicrophoneRequests = () => {
  window.__audioRequests = []
  const media = navigator.mediaDevices
  const native = media.getUserMedia.bind(media)
  media.getUserMedia = (constraints) => {
    const audio = constraints?.audio
    if (audio && typeof audio === 'object') {
      const id = audio.deviceId
      window.__audioRequests.push(typeof id === 'object' && id && 'exact' in id ? String(id.exact) : '')
    }
    return native(constraints)
  }
}

async function signIn(page: Page, username: string, password: string) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('Benutzername').fill(username)
  await dialog.getByLabel('Passwort').fill(password)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

test('right-clicking the microphone switches the input device with the fake devices', async ({ browser, browserName }) => {
  test.skip(browserName !== 'chromium', 'The fake devices are a Chromium flag set')
  const context = await browser.newContext({ permissions: ['microphone', 'camera'] })
  await context.addInitScript(trackPeerConnections)
  await context.addInitScript(recordMicrophoneRequests)
  const page = await context.newPage()

  await signIn(page, ADMIN_USER, ADMIN_PASSWORD)
  const channel = `Menu ${suffix}`
  expect((await apiFetch(page, 'POST', '/api/admin/channels', { name: channel, type: 'voice' })).status).toBe(201)
  await page.reload()
  await page.locator('[data-channel-type="voice"]', { hasText: channel }).click()
  await expectTalkLive(page)

  await page.locator('[data-testid="toggle-mute"]').click({ button: 'right' })
  const menu = page.getByRole('menu').first()
  await expect(menu).toBeVisible()
  await menu.locator('[data-submenu-trigger="mic-device"]').click()
  const devices = page.getByRole('menu', { name: 'Mikrofon' }).last()
  const choices = devices.getByRole('menuitemradio')
  // "System default" plus the fake microphones.
  await expect.poll(() => choices.count()).toBeGreaterThan(2)
  const target = choices.last()
  await expect(target).toHaveAttribute('aria-checked', 'false')
  await target.click()

  // Saved as the setting, and the running microphone moved to that device.
  const saved = await page.evaluate(() => localStorage.getItem('mnema_input_dev'))
  expect(saved).toBeTruthy()
  await expect.poll(() => page.evaluate(() => window.__audioRequests.at(-1))).toBe(saved)

  // The menu stays open for another choice; Escape closes it and gives focus back to the button.
  await expect(menu).toBeVisible()
  await page.keyboard.press('Escape')
  await page.keyboard.press('Escape')
  await expect(page.getByRole('menu')).toHaveCount(0)
  await expect(page.locator('[data-testid="toggle-mute"]')).toBeFocused()

  await leaveTalks([page])
  await context.close()
})
