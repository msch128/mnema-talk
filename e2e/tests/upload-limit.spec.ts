import { test, expect } from './fixtures'
import type { Page } from '@playwright/test'
import { apiFetch, responseString, suiteMember } from './helpers'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'Admin'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const CHANNEL = `uploads-${Date.now().toString(36)}`

async function signIn(page: Page, username: string, password: string) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByRole('heading', { name: 'Mnema Talk' })).toBeVisible()
  await dialog.getByLabel('Benutzername').fill(username)
  await dialog.getByLabel('Passwort').fill(password)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

test('the Owner lowers the upload limit and a member\'s large file is refused with a clear message', async ({ browser }) => {
  const aCtx = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  const bCtx = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  const a = await aCtx.newPage()
  const b = await bCtx.newPage()
  try {
    await signIn(a, ADMIN_USER, ADMIN_PASSWORD)
    expect((await apiFetch(a, 'POST', '/api/admin/channels', { name: CHANNEL, type: 'text' })).status).toBe(201)

    await b.goto('/')
    if (suiteMember.registered) {
      expect((await apiFetch(b, 'POST', '/api/auth/login', { username: suiteMember.username, password: suiteMember.password })).status).toBe(200)
    } else {
      const invite = await apiFetch(a, 'POST', '/api/admin/invites', { max_uses: 1 })
      expect((await apiFetch(b, 'POST', '/api/auth/register', { username: suiteMember.username, password: suiteMember.password, invite_code: responseString(invite, 'code') })).status).toBe(201)
      suiteMember.registered = true
    }
    await b.reload()
    await b.locator('[data-channel-type="text"]', { hasText: CHANNEL }).click()

    // The Owner lowers the global limit to 1 MB in the System tab.
    await a.goto('/admin/system')
    const box = a.getByTestId('upload-limit')
    await expect(box).toBeVisible()
    await box.getByLabel('Größte Datei').fill('1')
    await box.getByRole('button', { name: 'Speichern' }).click()
    await expect(box.getByLabel('Größte Datei')).toHaveValue('1')

    // The member's client learns the new limit live.
    await expect.poll(async () => (await apiFetch(b, 'GET', '/api/permissions')).json, { timeout: 10_000 })
      .toMatchObject({ max_upload_mb: 1 })

    // 2 MB is refused: the page says why, nothing is sent.
    const big = { name: 'big.png', mimeType: 'image/png', buffer: Buffer.alloc(2 * 1024 * 1024) }
    await b.locator('input[type="file"]').first().setInputFiles(big)
    await expect(b.getByText('Datei ist zu groß (max. 1 MB).').first()).toBeVisible()

    // The server enforces it too, whatever the client does.
    const direct = await b.evaluate(async (channelName: string) => {
      const channels = await (await fetch('/api/channels')).json() as { channels?: { id: string; name: string }[] } | { id: string; name: string }[]
      const flat = Array.isArray(channels) ? channels : (channels.channels ?? [])
      const id = flat.find(c => c.name === channelName)?.id
      const form = new FormData()
      form.append('file', new Blob([new Uint8Array(3 * 1024 * 1024)], { type: 'image/png' }), 'big.png')
      // The server may close the connection once it has refused, before the whole body is sent;
      // Chromium and WebKit still read the refusal, Firefox reports a network error instead (0).
      return fetch(`/api/channels/${id}/upload`, { method: 'POST', body: form }).then(res => res.status, () => 0)
    }, CHANNEL)
    expect([0, 400, 413]).toContain(direct)
  } finally {
    // Back to the server maximum for the other specs.
    await apiFetch(a, 'PUT', '/api/admin/upload-limit', { max_upload_mb: null })
    await aCtx.close()
    await bCtx.close()
  }
})
