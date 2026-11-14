import { request } from '@playwright/test'
import { test, expect } from './fixtures'
import { apiFetch, responseString } from './helpers'

// Giving a bot access to every channel in one step (doc/bots.md): the Owner
// adds a bot from its add link, chooses "All channels" and ticks nothing else.
// The bot then works in the channels created before and after, and a private
// channel stays hidden from it until the Owner lists it.
const ADMIN_USER = process.env.E2E_ADMIN_USER || 'cool_guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)

function base64url(text: string): string {
  return Buffer.from(text, 'utf8').toString('base64url')
}

test('the Owner adds a bot with access to all channels; private channels stay hidden from it', async ({ browser, baseURL }) => {
  test.setTimeout(90_000)
  const owner = await (await browser.newContext({ viewport: { width: 1200, height: 900 } })).newPage()
  await owner.goto('/')
  expect((await apiFetch(owner, 'POST', '/api/auth/login', { username: ADMIN_USER, password: ADMIN_PASSWORD })).status).toBe(200)

  const openName = `all-open-${suffix}`
  const privateName = `all-private-${suffix}`
  const openId = responseString(await apiFetch(owner, 'POST', '/api/admin/channels', { name: openName, type: 'text' }), 'id')
  const privateId = responseString(await apiFetch(owner, 'POST', '/api/admin/channels', { name: privateName, type: 'text' }), 'id')
  const roles = (await apiFetch(owner, 'GET', '/api/roles')).json as { id: string; is_everyone: boolean }[]
  const everyone = roles.find(r => r.is_everyone)?.id
  expect(everyone).toBeTruthy()
  expect((await apiFetch(owner, 'PUT', `/api/admin/channels/${privateId}/overwrites`, { overwrites: [{ type: 'role', id: everyone, allow: 0, deny: 2 }] })).status).toBe(204)

  const NAME = `Allbot ${suffix}`.slice(0, 24)
  const manifest = { manifest_version: 1, name: NAME, intents: [], permissions: ['view_channel', 'send_messages'], commands: [] }
  await owner.goto(`/admin/bots/add#manifest=${base64url(JSON.stringify(manifest))}`)
  const confirm = owner.getByTestId('manifest-confirm')
  await expect(confirm).toBeVisible()
  await expect(confirm.locator('[data-access="channels"]')).toBeChecked()
  await confirm.locator('[data-access="all"]').check()
  await expect(confirm.getByTestId('bot-access-private-note')).toBeVisible()
  await confirm.getByTestId('manifest-install').click()
  await expect(owner.getByTestId('bot-secret')).toBeVisible()
  const token = (await owner.getByTestId('bot-secret-value').innerText()).trim()
  await owner.getByTestId('bot-secret-done').click()

  // The settings show the choice that was made.
  const settings = owner.getByTestId('bot-channel-access')
  await expect(settings.locator('[data-access="all"]')).toBeChecked()

  const origin = baseURL ?? ''
  const bot = await request.newContext({ baseURL: origin, extraHTTPHeaders: { Authorization: `Bot ${token}` } })
  try {
    const names = async () => {
      const res = await bot.get('/api/bot/v1/channels')
      expect(res.status()).toBe(200)
      const tree = (await res.json()) as { uncategorized?: { name: string }[]; categories?: { channels: { name: string }[] }[] }
      return [...(tree.uncategorized ?? []), ...(tree.categories ?? []).flatMap(c => c.channels)].map(c => c.name)
    }
    await expect.poll(names).toContain(openName)
    expect(await names()).not.toContain(privateName)
    expect((await bot.post(`/api/bot/v1/channels/${openId}/messages`, { data: { content: `hello ${suffix}` } })).status()).toBe(201)
    expect((await bot.get(`/api/bot/v1/channels/${privateId}/messages`)).status()).toBe(404)

    // A channel created later is covered without touching the bot.
    const lateName = `all-late-${suffix}`
    await apiFetch(owner, 'POST', '/api/admin/channels', { name: lateName, type: 'text' })
    await expect.poll(names).toContain(lateName)

    // Listing the private channel under "Private channels too" opens it.
    await settings.getByTestId('bot-access-private').locator('summary').click()
    await settings.locator(`[data-channel="${privateId}"]`).check()
    await settings.getByTestId('bot-channel-access-save').click()
    await expect.poll(names).toContain(privateName)
  } finally {
    await bot.dispose()
    const bots = await apiFetch(owner, 'GET', '/api/admin/bots')
    const botId = (bots.json as { id: string; name: string }[]).find(b => b.name === NAME)?.id
    if (botId) await apiFetch(owner, 'DELETE', `/api/admin/bots/${botId}`)
  }
})
