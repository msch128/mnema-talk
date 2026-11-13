import type { Page } from '@playwright/test'
import { test, expect } from './fixtures'
import { request } from '@playwright/test'
import { apiFetch, responseString } from './helpers'

// The bot platform end to end (doc/bots.md): the Owner adds a bot from its
// manifest through the add link, the bot connects to the gateway from this test
// process (a Node WebSocket with the Authorization header, as a real bot runs),
// posts a message with a button and a select, and answers a click by updating
// the message (callback 7) and a pick by opening a form (callback 9), whose
// submission reaches it as interaction type 5.
const ADMIN_USER = process.env.E2E_ADMIN_USER || 'cool_guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
// A 1x1 PNG: the web app uploads it through the avatar route.
const PIXEL = 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYAAAAAYAAjCB0C8AAAAASUVORK5CYII='

interface Frame { op: number; s?: number | null; t?: string | null; d: unknown }
interface Interaction { id: string; token: string; type: number; data: { custom_id?: string; values?: string[]; components?: { components: { custom_id: string; value: string }[] }[] } }

function base64url(text: string): string {
  return Buffer.from(text, 'utf8').toString('base64url')
}

/** A gateway session of the bot: it identifies, heartbeats and keeps every dispatch. */
function connectGateway(baseURL: string, token: string) {
  const events: { t: string; d: unknown }[] = []
  const url = `${baseURL.replace(/^http/, 'ws')}/api/bot/v1/gateway`
  // Node's WebSocket (undici) takes headers in its init object; a browser's cannot.
  const Socket = WebSocket as unknown as new (url: string, init: { headers: Record<string, string> }) => WebSocket
  const ws = new Socket(url, { headers: { Authorization: `Bot ${token}` } })
  let seq: number | null = null
  let beat: ReturnType<typeof setInterval> | null = null
  ws.addEventListener('message', (ev: MessageEvent) => {
    const f = JSON.parse(String(ev.data)) as Frame
    if (typeof f.s === 'number') seq = f.s
    if (f.op === 10) {
      ws.send(JSON.stringify({ op: 2, d: { token, intents: 1 } }))
      beat = setInterval(() => ws.send(JSON.stringify({ op: 1, d: seq })), 20_000)
    } else if (f.op === 0 && f.t) {
      events.push({ t: f.t, d: f.d })
    }
  })
  return {
    events,
    interactions: () => events.filter(e => e.t === 'INTERACTION_CREATE').map(e => e.d as Interaction),
    close() {
      if (beat) clearInterval(beat)
      ws.close()
    }
  }
}

async function signIn(page: Page) {
  await page.goto('/')
  expect((await apiFetch(page, 'POST', '/api/auth/login', { username: ADMIN_USER, password: ADMIN_PASSWORD })).status).toBe(200)
}

test('the Owner adds a bot from its add link; its button updates the message and its select opens a form', async ({ browser, baseURL }) => {
  test.setTimeout(120_000)
  const origin = baseURL ?? ''
  const owner = await (await browser.newContext({ viewport: { width: 1400, height: 900 } })).newPage()
  await signIn(owner)
  const CHANNEL = `bot-platform-${suffix}`
  const channelId = responseString(await apiFetch(owner, 'POST', '/api/admin/channels', { name: CHANNEL, type: 'text' }), 'id')
  const NAME = `Mnemo ${suffix}`.slice(0, 24)
  const manifest = {
    manifest_version: 1, name: NAME, description: 'Answers questions and keeps notes.', homepage: 'https://example.org/mnemo',
    avatar: PIXEL, intents: ['GUILDS'], permissions: ['view_channel', 'send_messages'],
    commands: [{ name: 'ask', description: 'Ask something', options: [] }]
  }

  // The add link opens the confirm screen; the Owner ticks the one channel.
  await owner.goto(`/admin/bots/add#manifest=${base64url(JSON.stringify(manifest))}`)
  const confirm = owner.getByTestId('manifest-confirm')
  await expect(confirm).toBeVisible()
  await expect(owner).toHaveURL(/\/admin\/bots$/)
  await expect(confirm.getByTestId('manifest-name')).toHaveText(NAME)
  await expect(confirm.getByTestId('manifest-avatar')).toBeVisible()
  await expect(confirm.locator('[data-permission="send_messages"]')).toBeChecked()
  await expect(confirm.locator(`[data-channel="${channelId}"]`)).not.toBeChecked()
  await confirm.locator(`[data-channel="${channelId}"]`).check()
  await confirm.getByTestId('manifest-install').click()
  await expect(owner.getByTestId('bot-secret')).toBeVisible()
  const token = (await owner.getByTestId('bot-secret-value').innerText()).trim()
  expect(token.startsWith('mnb_')).toBe(true)
  await owner.getByTestId('bot-secret-done').click()
  await expect(owner.getByTestId('bot-detail')).toContainText('Answers questions and keeps notes.')
  await expect(owner.getByTestId('bot-avatar-preview')).toBeVisible()

  const gateway = connectGateway(origin, token)
  const bot = await request.newContext({ baseURL: origin, extraHTTPHeaders: { Authorization: `Bot ${token}` } })
  try {
    await expect.poll(() => gateway.events.map(e => e.t)).toContain('READY')
    const posted = await bot.post(`/api/bot/v1/channels/${channelId}/messages`, { data: {
      content: `Deploy ${suffix} to production?`,
      components: [
        { type: 1, components: [{ type: 2, style: 3, label: 'Deploy', custom_id: 'deploy' }, { type: 2, style: 5, label: 'Changelog', url: 'https://example.org/changes' }] },
        { type: 1, components: [{ type: 3, custom_id: 'env', placeholder: 'Environment', options: [{ label: 'Staging', value: 'staging' }, { label: 'Production', value: 'prod' }] }] }
      ]
    } })
    expect(posted.status()).toBe(201)
    const messageId = ((await posted.json()) as { id: string }).id

    await owner.goto('/')
    await owner.locator('[data-channel-type="text"]', { hasText: CHANNEL }).click()
    const row = owner.locator(`[data-msg-id="${messageId}"]`)
    await expect(row.getByTestId('message-components')).toBeVisible()

    // The click reaches the bot, which updates the message it is on.
    await row.getByTestId('component-button').filter({ hasText: 'Deploy' }).click()
    await expect.poll(() => gateway.interactions().map(i => `${i.type}:${i.data.custom_id ?? ''}`)).toContain('3:deploy')
    const click = gateway.interactions().find(i => i.data.custom_id === 'deploy')!
    const updated = await bot.post(`/api/bot/v1/interactions/${click.id}/${click.token}/callback`, { data: { type: 7, data: { content: `Deploying ${suffix}`, components: [] } } })
    expect(updated.status()).toBeLessThan(300)
    await expect(row).toContainText(`Deploying ${suffix}`)
    await expect(row.getByTestId('message-components')).toHaveCount(0)

    // A second message with the select: picking opens the bot's form.
    const second = await bot.post(`/api/bot/v1/channels/${channelId}/messages`, { data: {
      content: `Where to ${suffix}?`,
      components: [{ type: 1, components: [{ type: 3, custom_id: 'env', placeholder: 'Environment', options: [{ label: 'Staging', value: 'staging' }, { label: 'Production', value: 'prod' }] }] }]
    } })
    expect(second.status()).toBe(201)
    const secondRow = owner.locator(`[data-msg-id="${((await second.json()) as { id: string }).id}"]`)
    await secondRow.getByRole('button', { name: 'Environment' }).click()
    await secondRow.getByRole('option', { name: 'Production' }).click()
    await expect.poll(() => gateway.interactions().map(i => `${i.type}:${(i.data.values ?? []).join(',')}`)).toContain('3:prod')
    const pick = gateway.interactions().find(i => i.data.values?.includes('prod'))!
    const opened = await bot.post(`/api/bot/v1/interactions/${pick.id}/${pick.token}/callback`, { data: { type: 9, data: {
      custom_id: 'feedback', title: 'Feedback',
      components: [{ type: 1, components: [{ type: 4, custom_id: 'subject', label: 'Subject', style: 1, max_length: 80 }] }]
    } } })
    expect(opened.status()).toBeLessThan(300)
    const form = owner.getByTestId('bot-modal')
    await expect(form).toBeVisible()
    await form.locator('[data-input="subject"]').fill(`Login ${suffix}`)
    await form.getByTestId('bot-modal-submit').click()
    await expect(form).toHaveCount(0)
    await expect.poll(() => gateway.interactions().filter(i => i.type === 5).map(i => i.data.components?.[0]?.components[0]?.value)).toContain(`Login ${suffix}`)
  } finally {
    gateway.close()
    await bot.dispose()
    const bots = await apiFetch(owner, 'GET', '/api/admin/bots')
    const botId = (bots.json as { id: string; name: string }[]).find(b => b.name === NAME)?.id
    if (botId) await apiFetch(owner, 'DELETE', `/api/admin/bots/${botId}`)
  }
})

test('on a phone a bot’s rows of buttons stay apart as rows, and its form title is shown whole', async ({ browser, baseURL }) => {
  test.setTimeout(120_000)
  const origin = baseURL ?? ''
  const owner = await (await browser.newContext({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true })).newPage()
  await signIn(owner)
  const channelId = responseString(await apiFetch(owner, 'POST', '/api/admin/channels', { name: `bot-phone-${suffix}`, type: 'text' }), 'id')
  const NAME = `Grid ${suffix}`.slice(0, 24)
  const manifest = { manifest_version: 1, name: NAME, permissions: ['view_channel', 'send_messages'], intents: ['GUILDS'] }
  const installed = await apiFetch(owner, 'POST', '/api/admin/bots/install', { manifest, permissions: manifest.permissions, intents: manifest.intents, channel_ids: [channelId] })
  expect(installed.status).toBe(201)
  const { bot: created, token: issued } = installed.json as { bot: { id: string }; token: { secret: string } }
  const gateway = connectGateway(origin, issued.secret)
  const bot = await request.newContext({ baseURL: origin, extraHTTPHeaders: { Authorization: `Bot ${issued.secret}` } })
  try {
    await expect.poll(() => gateway.events.map(e => e.t)).toContain('READY')
    const rows = Array.from({ length: 5 }, (_, r) => ({ type: 1, components: Array.from({ length: 5 }, (_, c) => ({ type: 2, style: 2, label: `Option ${r}-${c}`, custom_id: `b${r}${c}` })) }))
    const posted = await bot.post(`/api/bot/v1/channels/${channelId}/messages`, { data: { content: `Grid ${suffix}`, components: rows } })
    expect(posted.status()).toBe(201)
    await owner.goto(`/c/${channelId}`)
    const message = owner.locator(`[data-msg-id="${((await posted.json()) as { id: string }).id}"]`)
    await expect(message.getByTestId('component-row')).toHaveCount(5)

    // Each row wraps onto two lines here; the space between rows must be clearly larger than inside one.
    const gaps = await message.getByTestId('component-row').evaluateAll(els => els.map(el => {
      const buttons = [...el.querySelectorAll('button')]
      const tops = [...new Set(buttons.map(b => Math.round(b.getBoundingClientRect().top)))].sort((a, b) => a - b)
      const height = buttons[0]?.getBoundingClientRect().height ?? 0
      const box = el.getBoundingClientRect()
      return { lines: tops.length, inside: tops.length > 1 ? (tops[1] ?? 0) - ((tops[0] ?? 0) + height) : 0, top: box.top, bottom: box.bottom }
    }))
    expect(gaps.every(g => g.lines > 1), 'the rows wrap on a phone').toBe(true)
    for (let i = 1; i < gaps.length; i++) {
      const before = gaps[i - 1]!
      expect(gaps[i]!.top - before.bottom, `gap between rows ${i - 1} and ${i}`).toBeGreaterThan(before.inside + 2)
    }

    // A 45-character form title wraps instead of being cut.
    const TITLE = 'Tell the bot about your deployment window now'
    expect(TITLE.length).toBe(45)
    await message.getByTestId('component-button').filter({ hasText: 'Option 0-0' }).click()
    await expect.poll(() => gateway.interactions().map(i => i.data.custom_id)).toContain('b00')
    const click = gateway.interactions().find(i => i.data.custom_id === 'b00')!
    const opened = await bot.post(`/api/bot/v1/interactions/${click.id}/${click.token}/callback`, { data: { type: 9, data: {
      custom_id: 'window', title: TITLE, components: [{ type: 1, components: [{ type: 4, custom_id: 'when', label: 'When', style: 1 }] }]
    } } })
    expect(opened.status()).toBeLessThan(300)
    const heading = owner.getByRole('dialog').getByRole('heading', { name: TITLE })
    await expect(heading).toBeVisible()
    const cut = await heading.evaluate(el => el.scrollWidth > el.clientWidth + 1 || getComputedStyle(el).textOverflow === 'ellipsis')
    expect(cut, 'the form title is cut').toBe(false)
  } finally {
    gateway.close()
    await bot.dispose()
    await apiFetch(owner, 'DELETE', `/api/admin/bots/${created.id}`)
  }
})
