import { test, expect } from './fixtures'
import type { Page } from '@playwright/test'
import { apiFetch, responseString } from './helpers'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'cool_guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const CHANNEL = `files-${Date.now().toString(36)}`

async function signIn(page: Page) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByTestId('login-heading')).toBeVisible()
  await dialog.getByLabel('Benutzername').fill(ADMIN_USER)
  await dialog.getByLabel('Passwort').fill(ADMIN_PASSWORD)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

// A small PNG of its own colour, made in the page and wrapped in a DataTransfer like a clipboard holds it.
const pictures = (page: Page, names: string[]) => page.evaluateHandle(async (fileNames: string[]) => {
  const transfer = new DataTransfer()
  for (const [i, name] of fileNames.entries()) {
    const canvas = document.createElement('canvas')
    canvas.width = 48
    canvas.height = 32
    const ctx = canvas.getContext('2d')!
    ctx.fillStyle = `hsl(${i * 90}, 60%, 50%)`
    ctx.fillRect(0, 0, 48, 32)
    const blob = await new Promise<Blob>(resolve => canvas.toBlob(b => resolve(b!), 'image/png'))
    transfer.items.add(new File([blob], name, { type: 'image/png' }))
  }
  return transfer
}, names)

test('three pasted pictures wait in the tray and go out as one message with a gallery and a viewer', async ({ browser }) => {
  const context = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  const page = await context.newPage()
  try {
    await signIn(page)
    const id = responseString(await apiFetch(page, 'POST', '/api/admin/channels', { name: CHANNEL, type: 'text' }), 'id')
    await page.goto(`/c/${id}`)
    const composer = page.getByRole('textbox', { name: `Nachricht an #${CHANNEL}` })
    await composer.focus()

    // Pasting only stages: the pictures upload in the background and nothing is posted.
    const clipboard = await pictures(page, ['red.png', 'green.png', 'blue.png'])
    await composer.evaluate((el, transfer) => {
      el.dispatchEvent(new ClipboardEvent('paste', { clipboardData: transfer, bubbles: true, cancelable: true }))
    }, clipboard)
    const tray = page.getByTestId('attachment-tray')
    await expect(tray.locator('li')).toHaveCount(3)
    await expect(tray.locator('li[data-attachment-status="ready"]')).toHaveCount(3)
    await expect(page.locator('.msg-list img')).toHaveCount(0)

    // Text and Enter send one message that carries all three, in tray order.
    await composer.fill('three colours')
    await composer.press('Enter')
    await expect(tray).toBeHidden()
    const message = page.locator('[data-msg-id]').filter({ hasText: 'three colours' })
    await expect(message).toHaveCount(1)
    const cells = message.locator('[data-gallery-cell] img')
    await expect(cells).toHaveCount(3)
    expect(await cells.evaluateAll(imgs => imgs.map(img => img.getAttribute('alt')))).toEqual(['red.png', 'green.png', 'blue.png'])

    // The viewer opens on the clicked picture and steps through the message's pictures.
    await cells.nth(1).click()
    const viewer = page.getByTestId('media-viewer')
    await expect(viewer).toBeVisible()
    await expect(viewer.getByTestId('viewer-name')).toHaveText('green.png')
    await expect(viewer.getByTestId('viewer-counter')).toHaveText('2 / 3')
    await page.keyboard.press('ArrowRight')
    await expect(viewer.getByTestId('viewer-name')).toHaveText('blue.png')
    await expect(viewer.getByTestId('viewer-counter')).toHaveText('3 / 3')
    await expect(viewer.getByTestId('viewer-sender')).toContainText(ADMIN_USER)
    await page.keyboard.press('ArrowLeft')
    await page.keyboard.press('ArrowLeft')
    await expect(viewer.getByTestId('viewer-name')).toHaveText('red.png')
    await page.keyboard.press('Escape')
    await expect(viewer).toBeHidden()
  } finally {
    await context.close()
  }
})
