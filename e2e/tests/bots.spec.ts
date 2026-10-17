import type { Page } from '@playwright/test'
import { test, expect, request } from '@playwright/test'
import { apiFetch, responseString } from './helpers'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'Herzog'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
const OPEN = `bot-open-${suffix}`
const CLOSED = `bot-closed-${suffix}`
const ROLE = `Bot Poster ${suffix}`
const BOT = `Helfer ${suffix}`

// Permission and intent bits (doc/roles-and-permissions.md, doc/bots.md).
const VIEW_CHANNEL = 1 << 1
const SEND_MESSAGES = 1 << 2
const ADD_REACTIONS = 1 << 4
const INTENT_GUILD_MESSAGES = 512

async function signIn(page: Page, username: string, password: string) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('Benutzername').fill(username)
  await dialog.getByLabel('Passwort').fill(password)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

test('the Owner creates a bot, scopes it to one channel and hands out a token; a script posts through the API', async ({ browser, baseURL }) => {
  const page = await (await browser.newContext({ viewport: { width: 1400, height: 900 } })).newPage()
  await signIn(page, ADMIN_USER, ADMIN_PASSWORD)

  const open = responseString(await apiFetch(page, 'POST', '/api/admin/channels', { name: OPEN, type: 'text' }), 'id')
  const closed = responseString(await apiFetch(page, 'POST', '/api/admin/channels', { name: CLOSED, type: 'text' }), 'id')
  const role = await apiFetch(page, 'POST', '/api/admin/roles', { name: ROLE, permissions: SEND_MESSAGES | ADD_REACTIONS })
  expect(role.status).toBe(201)
  const roleId = responseString(role, 'id')
  // Scope: only the open channel lets the bot's role in.
  const scope = await apiFetch(page, 'PUT', `/api/admin/channels/${open}/overwrites`, { overwrites: [{ type: 'role', id: roleId, allow: VIEW_CHANNEL, deny: 0 }] })
  expect(scope.status).toBe(204)

  // The bot, its role and its token, all through the admin area.
  await page.goto('/admin/bots')
  await expect(page.getByTestId('admin-bots')).toBeVisible()
  await page.getByTestId('bot-new-name').fill(BOT)
  await page.getByTestId('bot-create').click()
  await expect(page.getByTestId('bot-row').filter({ hasText: BOT })).toBeVisible()
  await page.getByTestId('bot-row').filter({ hasText: BOT }).click()
  await expect(page.getByTestId('bot-detail')).toBeVisible()

  // Without a role the bot reaches nothing: the access view is empty.
  await expect(page.getByTestId('bot-access-none')).toBeVisible()
  await page.locator(`[data-testid="bot-roles"] [data-role="${ROLE}"] input[type="checkbox"]`).check()
  await page.getByTestId('bot-roles-save').click()
  await expect(page.getByTestId('bot-access-channel').filter({ hasText: OPEN })).toBeVisible()
  await expect(page.getByTestId('bot-access-channel').filter({ hasText: CLOSED })).toHaveCount(0)

  await page.getByTestId('bot-tokens').getByRole('button', { name: 'Token ausstellen' }).click()
  await expect(page.getByTestId('bot-secret')).toBeVisible()
  const token = (await page.getByTestId('bot-secret-value').innerText()).trim()
  expect(token.startsWith('mnb_')).toBe(true)
  await expect(page.getByTestId('bot-secret-curl')).toContainText('Authorization: Bot')
  await page.getByTestId('bot-secret-done').click()
  await expect(page.getByTestId('bot-token').first()).toContainText(token.slice(-4))

  // A bare HTTP client: no cookies, no Origin header, only the token.
  await page.goto('/')
  await page.locator('[data-channel-type="text"]', { hasText: OPEN }).click()
  await expect(page.getByRole('textbox', { name: `Nachricht an #${OPEN}` })).toBeVisible()
  const bot = await request.newContext({ baseURL: baseURL ?? '', extraHTTPHeaders: { Authorization: `Bot ${token}` } })
  const text = `deploy ${suffix} finished`
  const sent = await bot.post(`/api/bot/v1/channels/${open}/messages`, { data: { content: text, embeds: [{ title: 'Run 7', description: 'all green' }] } })
  expect(sent.status()).toBe(201)
  await expect(page.getByText(text)).toBeVisible()
  await expect(page.getByTestId('bot-badge').first()).toBeVisible()

  // Everything outside its scope looks like it does not exist.
  const channels = await bot.get('/api/bot/v1/channels')
  expect(channels.status()).toBe(200)
  const listed = JSON.stringify(await channels.json())
  expect(listed).toContain(OPEN)
  expect(listed).not.toContain(CLOSED)
  const read = await bot.get(`/api/bot/v1/channels/${closed}/messages`)
  expect(read.status()).toBe(404)
  expect((await read.json() as { code: number }).code).toBe(10003)
  expect((await bot.post(`/api/bot/v1/channels/${closed}/messages`, { data: { content: 'nope' } })).status()).toBe(404)

  // A token is no session, and revoking it takes effect at once.
  expect((await bot.get('/api/channels')).status()).toBe(401)
  expect((await bot.get('/api/admin/bots')).status()).toBe(401)
  const bots = await apiFetch(page, 'GET', '/api/admin/bots')
  const botId = (bots.json as { id: string; name: string }[]).find(b => b.name === BOT)?.id
  expect(botId).toBeTruthy()
  const tokens = await apiFetch(page, 'GET', `/api/admin/bots/${botId}/tokens`)
  const tokenId = (tokens.json as { id: string }[])[0]?.id
  expect((await apiFetch(page, 'DELETE', `/api/admin/bots/${botId}/tokens/${tokenId}`)).status).toBe(204)
  expect((await bot.get('/api/bot/v1/users/@me')).status()).toBe(401)
  await bot.dispose()
})

test('a bot connects to the gateway, hears only its channel and answers a slash command', async ({ browser }) => {
  const page = await (await browser.newContext({ viewport: { width: 1400, height: 900 } })).newPage()
  await signIn(page, ADMIN_USER, ADMIN_PASSWORD)
  const name = `Echo ${suffix}`
  const open = responseString(await apiFetch(page, 'POST', '/api/admin/channels', { name: `gw-open-${suffix}`, type: 'text' }), 'id')
  const closed = responseString(await apiFetch(page, 'POST', '/api/admin/channels', { name: `gw-closed-${suffix}`, type: 'text' }), 'id')
  const roleId = responseString(await apiFetch(page, 'POST', '/api/admin/roles', { name: `Echo role ${suffix}`, permissions: SEND_MESSAGES }), 'id')
  expect((await apiFetch(page, 'PUT', `/api/admin/channels/${open}/overwrites`, { overwrites: [{ type: 'role', id: roleId, allow: VIEW_CHANNEL, deny: 0 }] })).status).toBe(204)
  const created = await apiFetch(page, 'POST', '/api/admin/bots', { name, allowed_intents: INTENT_GUILD_MESSAGES })
  expect(created.status).toBe(201)
  const botId = responseString(created, 'id')
  expect((await apiFetch(page, 'PUT', `/api/admin/bots/${botId}/roles`, { role_ids: [roleId] })).status).toBe(200)
  const token = responseString(await apiFetch(page, 'POST', `/api/admin/bots/${botId}/tokens`, {}), 'secret')

  // The bot registers a command and listens, in a page of its own (a browser WebSocket
  // cannot set headers, so the token goes in IDENTIFY).
  const botPage = await (await browser.newContext()).newPage()
  await botPage.goto('/')
  const registered = await botPage.evaluate(async ({ token }) => {
    const res = await fetch('/api/bot/v1/commands', {
      method: 'POST', headers: { Authorization: `Bot ${token}`, 'Content-Type': 'application/json' },
      body: JSON.stringify({ name: 'echo', description: 'Says it back', options: [{ name: 'text', description: 'What to say', type: 3, required: true }] }),
    })
    return res.status
  }, { token })
  expect(registered).toBe(200)
  await botPage.evaluate(({ token, intents }) => {
    const w = window as unknown as { __events: { t: string; d: unknown }[] }
    w.__events = []
    const ws = new WebSocket(`${location.origin.replace('http', 'ws')}/api/bot/v1/gateway`)
    let seq: number | null = null
    ws.onmessage = ev => {
      const f = JSON.parse(ev.data as string) as { op: number; s?: number; t?: string; d: unknown }
      if (f.s != null) seq = f.s
      if (f.op === 10) {
        ws.send(JSON.stringify({ op: 2, d: { token, intents } }))
        setInterval(() => ws.send(JSON.stringify({ op: 1, d: seq })), 20_000)
      } else if (f.op === 0 && f.t) {
        w.__events.push({ t: f.t, d: f.d })
        if (f.t === 'INTERACTION_CREATE') {
          const i = f.d as { id: string; token: string; data: { options: { value: string }[] } }
          void fetch(`/api/bot/v1/interactions/${i.id}/${i.token}/callback`, {
            method: 'POST', headers: { Authorization: `Bot ${token}`, 'Content-Type': 'application/json' },
            body: JSON.stringify({ type: 4, data: { content: `echo: ${i.data.options[0]?.value}` } }),
          })
        }
      }
    }
  }, { token, intents: INTENT_GUILD_MESSAGES })
  const heard = () => botPage.evaluate(() => (window as unknown as { __events: { t: string; d: { content?: string } }[] }).__events.map(e => `${e.t}:${e.d.content ?? ''}`))
  await expect.poll(heard).toContain('READY:')

  // It hears its channel, and not the other.
  expect((await apiFetch(page, 'POST', `/api/channels/${closed}/messages`, { content: `secret ${suffix}` })).status).toBe(201)
  expect((await apiFetch(page, 'POST', `/api/channels/${open}/messages`, { content: `public ${suffix}` })).status).toBe(201)
  await expect.poll(heard).toContain(`MESSAGE_CREATE:public ${suffix}`)
  expect((await heard()).join('|')).not.toContain(`secret ${suffix}`)

  // A member runs the command from the composer; the bot answers over REST.
  await page.goto('/')
  await page.locator('[data-channel-type="text"]', { hasText: `gw-open-${suffix}` }).click()
  const box = page.getByRole('textbox', { name: `Nachricht an #gw-open-${suffix}` })
  await box.fill('/ec')
  await expect(page.getByTestId('slash-suggestions')).toContainText('/echo')
  await box.press('Enter')
  await expect(page.getByTestId('slash-bar')).toBeVisible()
  await page.locator('[data-testid="slash-bar"] [data-option="text"]').fill('hallo')
  await page.getByTestId('slash-run').click()
  await expect(page.getByText('echo: hallo')).toBeVisible()
  await expect(page.getByTestId('bot-badge').first()).toBeVisible()
})
