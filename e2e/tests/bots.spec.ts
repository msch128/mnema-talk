import type { Browser, Page } from '@playwright/test'
import { test, expect } from './fixtures'
import { request } from '@playwright/test'
import { apiFetch, responseString, responseUserId, suiteMember } from './helpers'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'Herzog'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)

// Permission and intent bits (doc/roles-and-permissions.md, doc/bots.md).
const VIEW_CHANNEL = 1 << 1
const SEND_MESSAGES = 1 << 2
const ADD_REACTIONS = 1 << 4
const MANAGE_BOTS = 1 << 16
const INTENT_GUILD_MESSAGES = 512

// The Owner only needs a session for API calls: no app load, so the shared
// request budget of the administrator (600 a minute for the whole suite) stays
// with the specs that really drive the interface as the Owner.
async function signInForApi(page: Page, username: string, password: string) {
  await page.goto('/')
  expect((await apiFetch(page, 'POST', '/api/auth/login', { username, password })).status).toBe(200)
}

// The Owner sets things up through the API only (the suite shares the Owner's
// request budget); a member who may manage bots uses the web app.
async function ownerAndMember(browser: Browser) {
  const owner = await (await browser.newContext({ viewport: { width: 1400, height: 900 } })).newPage()
  const member = await (await browser.newContext({ viewport: { width: 1400, height: 900 } })).newPage()
  await signInForApi(owner, ADMIN_USER, ADMIN_PASSWORD)
  await member.goto('/')
  if (suiteMember.registered) {
    expect((await apiFetch(member, 'POST', '/api/auth/login', { username: suiteMember.username, password: suiteMember.password })).status).toBe(200)
  } else {
    const invite = await apiFetch(owner, 'POST', '/api/admin/invites', { max_uses: 1 })
    expect((await apiFetch(member, 'POST', '/api/auth/register', { username: suiteMember.username, password: suiteMember.password, invite_code: responseString(invite, 'code') })).status).toBe(201)
    suiteMember.registered = true
  }
  const memberId = responseUserId(await apiFetch(member, 'GET', '/api/auth/me'))
  return { owner, member, memberId }
}

test('a member who may manage bots creates one, scopes it to a channel and hands out a token; a script posts through the API', async ({ browser, baseURL }) => {
  const { owner, member, memberId } = await ownerAndMember(browser)
  const OPEN = `bot-open-${suffix}`
  const CLOSED = `bot-closed-${suffix}`
  const ROLE = `Bot Poster ${suffix}`
  const BOT = `Helfer ${suffix}`

  // Roles created later sit lower: the manager's role first, the bot's after it.
  const managers = await apiFetch(owner, 'POST', '/api/admin/roles', { name: `Bot managers ${suffix}`, permissions: MANAGE_BOTS | SEND_MESSAGES | ADD_REACTIONS })
  expect(managers.status).toBe(201)
  const managersId = responseString(managers, 'id')
  const role = await apiFetch(owner, 'POST', '/api/admin/roles', { name: ROLE, permissions: SEND_MESSAGES | ADD_REACTIONS })
  expect(role.status).toBe(201)
  const roleId = responseString(role, 'id')
  const open = responseString(await apiFetch(owner, 'POST', '/api/admin/channels', { name: OPEN, type: 'text' }), 'id')
  const closed = responseString(await apiFetch(owner, 'POST', '/api/admin/channels', { name: CLOSED, type: 'text' }), 'id')
  // Scope: only the open channel lets the bot's role in.
  expect((await apiFetch(owner, 'PUT', `/api/admin/channels/${open}/overwrites`, { overwrites: [{ type: 'role', id: roleId, allow: VIEW_CHANNEL, deny: 0 }] })).status).toBe(204)
  expect((await apiFetch(owner, 'PUT', `/api/admin/users/${memberId}/roles`, { role_ids: [managersId] })).status).toBe(204)

  try {
    // The bot, its role and its token, all through the admin area.
    await member.goto('/admin/bots')
    await expect(member.getByTestId('admin-bots')).toBeVisible()
    await member.getByTestId('bot-new-name').fill(BOT)
    await member.getByTestId('bot-create').click()
    await expect(member.getByTestId('bot-row').filter({ hasText: BOT })).toBeVisible()
    await member.getByTestId('bot-row').filter({ hasText: BOT }).click()
    await expect(member.getByTestId('bot-detail')).toBeVisible()

    // Without a role the bot reaches nothing: the access view is empty.
    await expect(member.getByTestId('bot-access-none')).toBeVisible()
    await member.locator(`[data-testid="bot-roles"] [data-role="${ROLE}"] input[type="checkbox"]`).check()
    await member.getByTestId('bot-roles-save').click()
    await expect(member.getByTestId('bot-access-channel').filter({ hasText: OPEN })).toBeVisible()
    await expect(member.getByTestId('bot-access-channel').filter({ hasText: CLOSED })).toHaveCount(0)

    await member.getByTestId('bot-tokens').getByRole('button', { name: 'Token ausstellen' }).click()
    await expect(member.getByTestId('bot-secret')).toBeVisible()
    const token = (await member.getByTestId('bot-secret-value').innerText()).trim()
    expect(token.startsWith('mnb_')).toBe(true)
    await expect(member.getByTestId('bot-secret-curl')).toContainText('Authorization: Bot')
    await member.getByTestId('bot-secret-done').click()
    await expect(member.getByTestId('bot-token').first()).toContainText(token.slice(-4))

    // A bare HTTP client: no cookies, no Origin header, only the token.
    await member.goto('/')
    await member.locator('[data-channel-type="text"]', { hasText: OPEN }).click()
    await expect(member.getByRole('textbox', { name: `Nachricht an #${OPEN}` })).toBeVisible()
    const bot = await request.newContext({ baseURL: baseURL ?? '', extraHTTPHeaders: { Authorization: `Bot ${token}` } })
    const text = `deploy ${suffix} finished`
    const sent = await bot.post(`/api/bot/v1/channels/${open}/messages`, { data: { content: text, embeds: [{ title: 'Run 7', description: 'all green' }] } })
    expect(sent.status()).toBe(201)
    await expect(member.getByText(text)).toBeVisible()
    await expect(member.getByTestId('bot-badge').first()).toBeVisible()

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
    const bots = await apiFetch(member, 'GET', '/api/admin/bots')
    const botId = (bots.json as { id: string; name: string }[]).find(b => b.name === BOT)?.id
    expect(botId).toBeTruthy()
    const tokens = await apiFetch(member, 'GET', `/api/admin/bots/${botId}/tokens`)
    const tokenId = (tokens.json as { id: string }[])[0]?.id
    expect((await apiFetch(member, 'DELETE', `/api/admin/bots/${botId}/tokens/${tokenId}`)).status).toBe(204)
    expect((await bot.get('/api/bot/v1/users/@me')).status()).toBe(401)
    await bot.dispose()
    expect((await apiFetch(member, 'DELETE', `/api/admin/bots/${botId}`)).status).toBe(204)
  } finally {
    // The shared member goes back to what it was.
    await apiFetch(owner, 'PUT', `/api/admin/users/${memberId}/roles`, { role_ids: [] })
  }
})

test('a bot connects to the gateway, hears only its channel and answers a slash command', async ({ browser }) => {
  const { owner, member } = await ownerAndMember(browser)
  const name = `Echo ${suffix}`
  const OPEN = `gw-open-${suffix}`
  const open = responseString(await apiFetch(owner, 'POST', '/api/admin/channels', { name: OPEN, type: 'text' }), 'id')
  const closed = responseString(await apiFetch(owner, 'POST', '/api/admin/channels', { name: `gw-closed-${suffix}`, type: 'text' }), 'id')
  const roleId = responseString(await apiFetch(owner, 'POST', '/api/admin/roles', { name: `Echo role ${suffix}`, permissions: SEND_MESSAGES }), 'id')
  expect((await apiFetch(owner, 'PUT', `/api/admin/channels/${open}/overwrites`, { overwrites: [{ type: 'role', id: roleId, allow: VIEW_CHANNEL, deny: 0 }] })).status).toBe(204)
  const created = await apiFetch(owner, 'POST', '/api/admin/bots', { name, allowed_intents: INTENT_GUILD_MESSAGES })
  expect(created.status).toBe(201)
  const botId = responseString(created, 'id')
  expect((await apiFetch(owner, 'PUT', `/api/admin/bots/${botId}/roles`, { role_ids: [roleId] })).status).toBe(200)
  const token = responseString(await apiFetch(owner, 'POST', `/api/admin/bots/${botId}/tokens`, {}), 'secret')

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
          // Only well-formed ids go into the path.
          if (!/^[0-9a-f-]{36}$/.test(i.id) || !/^[A-Za-z0-9_-]+$/.test(i.token)) return
          void fetch(`/api/bot/v1/interactions/${encodeURIComponent(i.id)}/${encodeURIComponent(i.token)}/callback`, {
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
  expect((await apiFetch(member, 'POST', `/api/channels/${closed}/messages`, { content: `secret ${suffix}` })).status).toBe(201)
  expect((await apiFetch(member, 'POST', `/api/channels/${open}/messages`, { content: `public ${suffix}` })).status).toBe(201)
  await expect.poll(heard).toContain(`MESSAGE_CREATE:public ${suffix}`)
  expect((await heard()).join('|')).not.toContain(`secret ${suffix}`)

  // A member runs the command from the composer; the bot answers over REST.
  await member.goto('/')
  await member.locator('[data-channel-type="text"]', { hasText: OPEN }).click()
  const box = member.getByRole('textbox', { name: `Nachricht an #${OPEN}` })
  await box.fill('/ec')
  await expect(member.getByTestId('slash-suggestions')).toContainText('/echo')
  await box.press('Enter')
  await expect(member.getByTestId('slash-bar')).toBeVisible()
  await member.locator('[data-testid="slash-bar"] [data-option="text"]').fill('hallo')
  await member.getByTestId('slash-run').click()
  await expect(member.getByText('echo: hallo')).toBeVisible()
  await expect(member.getByTestId('bot-badge').first()).toBeVisible()
  expect((await apiFetch(owner, 'DELETE', `/api/admin/bots/${botId}`)).status).toBe(204)
})
