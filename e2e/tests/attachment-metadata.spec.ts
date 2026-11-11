import { readFile } from 'node:fs/promises'
import path from 'node:path'
import { test, expect } from './fixtures'
import type { Page } from '@playwright/test'
import { apiFetch, responseString } from './helpers'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'cool_guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const CHANNEL = `photos-${Date.now().toString(36)}`
// A photo of 8x4 pixels as a phone writes it: turned by its EXIF orientation (6, so it is
// shown 4x8), with a GPS position, a camera make and an XMP packet. Invented data.
const PHOTO = path.join(process.cwd(), '..', 'internal', 'imgstrip', 'testdata', 'phone-orientation-6.jpg')
const MARKS = ['GPS-FIXTURE-CAM', 'xmp-fixture-location', 'iptc-fixture-byline', 'text-fixture-comment']

async function signIn(page: Page) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByTestId('login-heading')).toBeVisible()
  await dialog.getByLabel('Benutzername').fill(ADMIN_USER)
  await dialog.getByLabel('Passwort').fill(ADMIN_PASSWORD)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

// How the browser shows a picture: its size once the orientation is applied, and its pixels.
type Shown = { width: number, height: number, pixels: number[] }

test('a photo is stored without its position and shown turned the way it was sent', async ({ browser }) => {
  const context = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  const page = await context.newPage()
  try {
    const photo = (await readFile(PHOTO)).toString('base64')
    await signIn(page)
    const id = responseString(await apiFetch(page, 'POST', '/api/admin/channels', { name: CHANNEL, type: 'text' }), 'id')
    await page.goto(`/c/${id}`)
    const composer = page.getByRole('textbox', { name: `Nachricht an #${CHANNEL}` })
    await composer.focus()
    await composer.evaluate((el, base64) => {
      const bytes = Uint8Array.from(atob(base64), c => c.charCodeAt(0))
      const transfer = new DataTransfer()
      transfer.items.add(new File([bytes], 'holiday.jpg', { type: 'image/jpeg' }))
      el.dispatchEvent(new ClipboardEvent('paste', { clipboardData: transfer, bubbles: true, cancelable: true }))
    }, photo)
    await expect(page.getByTestId('attachment-tray').locator('li[data-attachment-status="ready"]')).toHaveCount(1)
    await composer.fill('from the trip')
    await composer.press('Enter')
    const picture = page.locator('[data-msg-id]').filter({ hasText: 'from the trip' }).locator('img[alt="holiday.jpg"]')
    await expect(picture).toHaveCount(1)
    const src = await picture.evaluate((img: HTMLImageElement) => img.src)

    const result = await page.evaluate(async ({ base64, url, marks }) => {
      const show = async (blob: Blob): Promise<Shown> => {
        const img = new Image()
        img.src = URL.createObjectURL(blob)
        await img.decode()
        const canvas = document.createElement('canvas')
        canvas.width = img.naturalWidth
        canvas.height = img.naturalHeight
        const ctx = canvas.getContext('2d')!
        ctx.drawImage(img, 0, 0)
        return { width: img.naturalWidth, height: img.naturalHeight, pixels: Array.from(ctx.getImageData(0, 0, canvas.width, canvas.height).data) }
      }
      const sent = Uint8Array.from(atob(base64), c => c.charCodeAt(0))
      const stored = new Uint8Array(await (await fetch(url)).arrayBuffer())
      const text = new TextDecoder('latin1').decode(stored)
      return {
        sentBytes: sent.length,
        storedBytes: stored.length,
        left: marks.filter(mark => text.includes(mark)),
        exifRecords: text.split('Exif\0\0').length - 1,
        sent: await show(new Blob([sent], { type: 'image/jpeg' })),
        stored: await show(new Blob([stored], { type: 'image/jpeg' }))
      }
    }, { base64: photo, url: src, marks: MARKS })

    // Nothing of the metadata is in what the server hands out; the one EXIF record left is the orientation.
    expect(result.left).toEqual([])
    expect(result.exifRecords).toBe(1)
    expect(result.storedBytes).toBeLessThan(result.sentBytes)
    // The browser turns the stored photo exactly as it turns the original, and draws the same pixels.
    expect([result.sent.width, result.sent.height]).toEqual([4, 8])
    expect([result.stored.width, result.stored.height]).toEqual([4, 8])
    expect(result.stored.pixels).toEqual(result.sent.pixels)
  } finally {
    await context.close()
  }
})
