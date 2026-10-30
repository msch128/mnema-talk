import { test, expect } from './fixtures'
import type { Page } from '@playwright/test'
import { apiFetch, responseString } from './helpers'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'cool_guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const CHANNEL = `composer-${Date.now().toString(36)}`
const VIEWPORT_HEIGHT = 900

async function signIn(page: Page) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByTestId('login-heading')).toBeVisible()
  await dialog.getByLabel('Benutzername').fill(ADMIN_USER)
  await dialog.getByLabel('Passwort').fill(ADMIN_PASSWORD)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

// A small PNG made in the page, wrapped in a DataTransfer the way a browser hands it over.
const fileTransfer = (page: Page, name: string) => page.evaluateHandle(async (fileName: string) => {
  const canvas = document.createElement('canvas')
  canvas.width = 32
  canvas.height = 32
  canvas.getContext('2d')!.fillRect(0, 0, 32, 32)
  const blob = await new Promise<Blob>(resolve => canvas.toBlob(b => resolve(b!), 'image/png'))
  const transfer = new DataTransfer()
  transfer.items.add(new File([blob], fileName, { type: 'image/png' }))
  return transfer
}, name)

test('the composer grows with its text and stages pasted and dropped images', async ({ browser }) => {
  const context = await browser.newContext({ viewport: { width: 1400, height: VIEWPORT_HEIGHT } })
  const page = await context.newPage()
  try {
    await signIn(page)
    const id = responseString(await apiFetch(page, 'POST', '/api/admin/channels', { name: CHANNEL, type: 'text' }), 'id')
    await page.goto(`/c/${id}`)
    const composer = page.getByRole('textbox', { name: `Nachricht an #${CHANNEL}` })
    const height = async () => (await composer.boundingBox())?.height ?? 0

    // One line, then more; at most 45 % of the window, then it scrolls inside.
    const oneLine = await height()
    await composer.fill('a\nb\nc\nd')
    await expect.poll(height).toBeGreaterThan(oneLine + 40)
    await composer.fill(Array.from({ length: 80 }, (_, i) => `line ${i}`).join('\n'))
    await expect.poll(height).toBeLessThanOrEqual(VIEWPORT_HEIGHT * 0.45 + 1)
    await expect.poll(height).toBeGreaterThan(VIEWPORT_HEIGHT * 0.4)
    expect(await composer.evaluate(el => el.scrollHeight > el.clientHeight)).toBe(true)
    await composer.fill('')
    await expect.poll(height).toBe(oneLine)

    // Text pastes as text and attaches nothing.
    await composer.focus()
    await composer.evaluate(el => {
      const transfer = new DataTransfer()
      transfer.setData('text/plain', 'just text')
      el.dispatchEvent(new ClipboardEvent('paste', { clipboardData: transfer, bubbles: true, cancelable: true }))
    })
    await expect(page.locator('.msg-list img')).toHaveCount(0)

    // A pasted image waits in the tray above the composer; nothing is sent.
    const tray = page.getByTestId('attachment-tray')
    const pasted = await fileTransfer(page, 'pasted.png')
    await composer.evaluate((el, transfer) => {
      el.dispatchEvent(new ClipboardEvent('paste', { clipboardData: transfer, bubbles: true, cancelable: true }))
    }, pasted)
    await expect(tray.locator('li')).toHaveCount(1)
    await expect(page.locator('.msg-list img')).toHaveCount(0)

    // A dropped image shows the calm veil while dragged and joins the tray on drop.
    const dropped = await fileTransfer(page, 'dropped.png')
    const chat = page.locator('#main-content')
    await chat.dispatchEvent('dragenter', { dataTransfer: dropped })
    await expect(page.getByTestId('file-drop-overlay')).toBeVisible()
    await chat.dispatchEvent('drop', { dataTransfer: dropped })
    await expect(page.getByTestId('file-drop-overlay')).toBeHidden()
    await expect(tray.locator('li')).toHaveCount(2)
    await expect(page.locator('.msg-list img')).toHaveCount(0)
  } finally {
    await context.close()
  }
})
