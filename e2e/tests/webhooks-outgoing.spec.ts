import { createHmac } from 'node:crypto'
import { createServer } from 'node:http'
import type { IncomingHttpHeaders } from 'node:http'
import type { Page } from '@playwright/test'
import { test, expect } from '@playwright/test'
import { apiFetch, responseString } from './helpers'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'Herzog'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)

async function signIn(page: Page, username: string, password: string) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('Benutzername').fill(username)
  await dialog.getByLabel('Passwort').fill(password)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

// e2e/run.sh starts the server with WEBHOOK_ALLOW_PRIVATE_TARGETS=127.0.0.0/8,
// so the receiver below on the loopback interface may be contacted. Real
// installations leave that variable empty.
test('an outgoing webhook delivers a signed event to a local receiver', async ({ browser }) => {
  const page = await (await browser.newContext({ viewport: { width: 1400, height: 900 } })).newPage()
  await signIn(page, ADMIN_USER, ADMIN_PASSWORD)
  const name = `out-${suffix}`
  const channel = await apiFetch(page, 'POST', '/api/admin/channels', { name, type: 'text' })
  expect(channel.status).toBe(201)
  const channelId = responseString(channel, 'id')

  const received: Array<{ headers: IncomingHttpHeaders; body: string }> = []
  const server = createServer((req, res) => {
    const chunks: Buffer[] = []
    req.on('data', (c: Buffer) => chunks.push(c))
    req.on('end', () => {
      received.push({ headers: req.headers, body: Buffer.concat(chunks).toString('utf8') })
      res.writeHead(204).end()
    })
  })
  await new Promise<void>(resolve => server.listen(0, '127.0.0.1', resolve))
  try {
    const address = server.address()
    if (address === null || typeof address === 'string') throw new Error('Expected a TCP address')
    const hook = await apiFetch(page, 'POST', `/api/admin/channels/${channelId}/outgoing-webhooks`, {
      name: 'Receiver', url: `http://127.0.0.1:${address.port}/hook`, events: ['MESSAGE_CREATE'],
    })
    expect(hook.status).toBe(201)
    const secret = responseString(hook, 'secret')

    await page.locator('[data-channel-type="text"]', { hasText: name }).click()
    const composer = page.getByRole('textbox', { name: `Nachricht an #${name}` })
    const text = `outgoing ${suffix}`
    await composer.fill(text)
    await composer.press('Enter')

    await expect.poll(() => received.length, { timeout: 15_000 }).toBeGreaterThan(0)
    const first = received[0]
    if (first === undefined) throw new Error('Expected a delivery')
    expect(first.headers['x-mnema-event']).toBe('MESSAGE_CREATE')
    const [t, v1] = String(first.headers['x-mnema-signature']).split(',').map(part => part.slice(part.indexOf('=') + 1))
    expect(v1).toBe(createHmac('sha256', secret).update(`${t}.${first.body}`).digest('hex'))
    expect(JSON.parse(first.body)).toMatchObject({ t: 'MESSAGE_CREATE', channel_id: channelId, d: { content: text } })
  } finally {
    await new Promise<void>(resolve => server.close(() => resolve()))
  }
})
