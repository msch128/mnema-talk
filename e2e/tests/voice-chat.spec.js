import { randomBytes } from 'node:crypto'
import { test, expect } from '@playwright/test'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'Herzog'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
const USER = `chatter_${suffix}`
const USER_PASSWORD = `pw-${suffix}-${randomBytes(8).toString('hex')}`
const CHANNEL = `Runde ${suffix}`

async function apiFetch(page, method, path, body) {
  return page.evaluate(async ({ method, path, body }) => {
    const res = await fetch(path, { method, credentials: 'same-origin', headers: body ? { 'Content-Type': 'application/json' } : {}, body: body ? JSON.stringify(body) : undefined })
    return { status: res.status, json: await res.json().catch(() => null) }
  }, { method, path, body })
}

async function signIn(page, username, password) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('Benutzername').fill(username)
  await dialog.getByLabel('Passwort').fill(password)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

// E2E_SHOT_DIR=<dir> saves screenshots of the steps for a look by eye.
async function shot(page, name) {
  if (process.env.E2E_SHOT_DIR) await page.screenshot({ path: `${process.env.E2E_SHOT_DIR}/${name}.png` })
}

test('the Talk has its own text chat under the stage', async ({ browser }) => {
  const aCtx = await browser.newContext({ permissions: ['microphone', 'camera'], viewport: { width: 1400, height: 900 } })
  const bCtx = await browser.newContext({ permissions: ['microphone', 'camera'], viewport: { width: 1400, height: 900 } })
  const a = await aCtx.newPage()
  const b = await bCtx.newPage()
  for (const [name, page] of [['a', a], ['b', b]]) {
    page.on('console', (m) => { if (['warning', 'error'].includes(m.type())) console.log(`[${name}] ${m.text()}`) })
    page.on('pageerror', (e) => console.log(`[${name}] pageerror ${e.message}`))
  }

  await signIn(a, ADMIN_USER, ADMIN_PASSWORD)
  const created = await apiFetch(a, 'POST', '/api/admin/channels', { name: CHANNEL, type: 'voice' })
  expect(created.status).toBe(201)
  const channelId = created.json.id
  const invite = await apiFetch(a, 'POST', '/api/admin/invites', { max_uses: 1 })
  await b.goto('/')
  expect((await apiFetch(b, 'POST', '/api/auth/register', { username: USER, password: USER_PASSWORD, invite_code: invite.json.code })).status).toBe(201)
  await b.reload()

  // Both join the Talk; the chat is closed by default.
  const sidebarVoice = (page) => page.locator('[data-channel-type="voice"]', { hasText: CHANNEL })
  await sidebarVoice(a).click()
  await sidebarVoice(b).click()
  for (const page of [a, b]) {
    await expect(page.getByRole('button', { name: 'Verlassen' }).first()).toBeVisible()
    await expect(page.getByTestId('voice-chat-panel')).toHaveCount(0)
    await expect(page).toHaveURL(new RegExp(`/v/${channelId}$`))
  }

  // A opens it with the chat button in the Talk header: Discord's empty state.
  await a.getByTestId('voice-chat-toggle').click()
  const aPanel = a.getByTestId('voice-chat-panel')
  await expect(aPanel).toBeVisible()
  await expect(a).toHaveURL(new RegExp(`/v/${channelId}/chat$`))
  await expect(aPanel.getByText(`Willkommen in ${CHANNEL}!`)).toBeVisible()
  await expect(aPanel.getByText(`Das ist der Anfang des Kanals ${CHANNEL}.`)).toBeVisible()
  // The stage stays above the chat with the control dock, not covered by it.
  const stageBox = await a.locator('main').first().boundingBox()
  const panelBox = await aPanel.boundingBox()
  expect(stageBox.y + stageBox.height).toBeLessThanOrEqual(panelBox.y + 1)
  await expect(a.getByRole('button', { name: 'Verlassen' }).first()).toBeVisible()

  // A writes; the message really reaches the server and B.
  const first = `Hört ihr mich? ${suffix}`
  const aComposer = aPanel.getByRole('textbox', { name: `Nachricht an ${CHANNEL}` })
  await aComposer.fill(first)
  await aComposer.press('Enter')
  await expect(aPanel.getByText(first)).toBeVisible()
  await expect(aComposer).toHaveValue('')
  // (The server used to refuse it with this error toast.)
  await expect(a.getByText('voice channels have no text chat')).toHaveCount(0)

  // B's chat is closed: the chat button and the sidebar row count it as unread.
  await expect(b.getByTestId('voice-chat-unread')).toHaveText('1')
  await expect(sidebarVoice(b).getByTestId('channel-unread')).toHaveText('1')
  await shot(b, 'chatpanel-closed-unread')

  // B opens the chat: the message is there and the badge is gone.
  await b.getByTestId('voice-chat-toggle').click()
  const bPanel = b.getByTestId('voice-chat-panel')
  await expect(bPanel.getByText(first)).toBeVisible()
  await expect(b.getByTestId('voice-chat-unread')).toHaveCount(0)

  // B answers; A sees it live.
  const answer = `Laut und deutlich ${suffix}`
  const bComposer = bPanel.getByRole('textbox', { name: `Nachricht an ${CHANNEL}` })
  await bComposer.fill(answer)
  await bComposer.press('Enter')
  await expect(aPanel.getByText(answer)).toBeVisible()
  await shot(a, 'chatpanel-open-messages')

  // The chat's height is dragged on the handle between stage and chat, and kept.
  const handle = aPanel.getByRole('separator', { name: 'Höhe des Chats anpassen' })
  const before = (await aPanel.boundingBox()).height
  const hb = await handle.boundingBox()
  await a.mouse.move(hb.x + hb.width / 2, hb.y + hb.height / 2)
  await a.mouse.down()
  await a.mouse.move(hb.x + hb.width / 2, hb.y + hb.height / 2 - 120, { steps: 6 })
  await a.mouse.up()
  await expect.poll(async () => Math.round((await aPanel.boundingBox()).height)).toBe(Math.round(before + 120))
  await handle.focus()
  await a.keyboard.press('ArrowDown')
  await expect.poll(async () => Math.round((await aPanel.boundingBox()).height)).toBe(Math.round(before + 112))

  // Remembered: after a reload the chat is open at that height.
  await a.reload()
  await expect(aPanel.getByText(answer)).toBeVisible()
  await expect.poll(async () => Math.round((await aPanel.boundingBox()).height)).toBe(Math.round(before + 112))

  // The X in the chat's header closes it; closed is remembered too.
  await aPanel.getByRole('button', { name: 'Chat schließen' }).click()
  await expect(aPanel).toHaveCount(0)
  await expect(a).toHaveURL(new RegExp(`/v/${channelId}$`))
  await a.reload()
  await expect(a.getByTestId('voice-chat-toggle')).toBeVisible()
  await expect(aPanel).toHaveCount(0)

  // A deep link opens it.
  await a.goto(`/v/${channelId}/chat`)
  await expect(aPanel.getByText(first)).toBeVisible()

  // On a narrow window it sits under the stage as well.
  await a.setViewportSize({ width: 760, height: 900 })
  await expect(aPanel).toBeVisible()
  await shot(a, 'chatpanel-narrow')

  await aCtx.close()
  await bCtx.close()
})
