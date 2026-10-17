import type { Page } from '@playwright/test'
import { test, expect, request } from '@playwright/test'
import { apiFetch, responseString } from './helpers'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'Herzog'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
const CHANNEL = `hooks-${suffix}`

async function signIn(page: Page, username: string, password: string) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('Benutzername').fill(username)
  await dialog.getByLabel('Passwort').fill(password)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

test('a script posts through a webhook and the message appears live', async ({ browser, baseURL }) => {
  const page = await (await browser.newContext({ viewport: { width: 1400, height: 900 } })).newPage()
  await signIn(page, ADMIN_USER, ADMIN_PASSWORD)
  const channel = await apiFetch(page, 'POST', '/api/admin/channels', { name: CHANNEL, type: 'text' })
  expect(channel.status).toBe(201)
  const channelId = responseString(channel, 'id')
  const hook = await apiFetch(page, 'POST', `/api/admin/channels/${channelId}/webhooks`, { name: 'Pipeline' })
  expect(hook.status).toBe(201)
  const url = responseString(hook, 'url')
  expect(url.startsWith(baseURL ?? '')).toBe(true)

  await page.locator('[data-channel-type="text"]', { hasText: CHANNEL }).click()
  await expect(page.getByRole('textbox', { name: `Nachricht an #${CHANNEL}` })).toBeVisible()

  // A bare HTTP client: no cookies, no Origin header, only the secret URL.
  const script = await request.newContext()
  const text = `build ${suffix} passed`
  const sent = await script.post(url, {
    data: {
      content: text,
      username: 'CI',
      embeds: [{ title: 'Run 42', description: 'all green', color: 65280, fields: [{ name: 'tests', value: '812', inline: true }] }],
    },
  })
  expect(sent.status()).toBe(204)

  await expect(page.getByText(text)).toBeVisible()
  await expect(page.getByTestId('bot-badge')).toBeVisible()
  await expect(page.getByTestId('embed-title')).toHaveText('Run 42')
  await expect(page.getByTestId('embed-field')).toContainText('812')

  // Wrong secret: nothing is posted.
  const wrong = await script.post(`${url}x`, { data: { content: 'nope' } })
  expect(wrong.status()).toBe(401)
  await expect(page.getByText('nope')).toHaveCount(0)
  await script.dispose()
})
