import type { Page } from '@playwright/test'
import { test, expect } from './fixtures'
import { apiFetch, responseString, suiteMember } from './helpers'

// A bot command with default_member_permissions: a member without the
// permission neither sees it in the suggestions nor gets it from the
// command list of the channel, a member with the permission (here the Owner) does, and a
// command without the field stays open to everyone. The bot only registers
// through REST; the gateway is not needed.

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'cool_guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
const CHANNEL = `cmdperm-${suffix}`
const OPEN_COMMAND = `hello${suffix.replace(/[^a-z0-9]/g, '')}`.slice(0, 30)
const MOD_COMMAND = `purge${suffix.replace(/[^a-z0-9]/g, '')}`.slice(0, 30)
// Permission bits (doc/roles-and-permissions.md).
const VIEW_CHANNEL = 1 << 1
const SEND_MESSAGES = 1 << 2
const VIEWPORT = { width: 1400, height: 900 }

async function signIn(page: Page, username: string, password: string) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByTestId('login-heading')).toBeVisible()
  await dialog.getByLabel('Benutzername').fill(username)
  await dialog.getByLabel('Passwort').fill(password)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

async function registerCommand(page: Page, token: string, body: Record<string, unknown>): Promise<number> {
  return page.evaluate(async ({ token, body }) => {
    const res = await fetch('/api/bot/v1/commands', {
      method: 'POST', headers: { Authorization: `Bot ${token}`, 'Content-Type': 'application/json' }, body: JSON.stringify(body),
    })
    return res.status
  }, { token, body })
}

async function suggestionsFor(page: Page, prefix: string) {
  const box = page.getByRole('textbox', { name: `Nachricht an #${CHANNEL}` })
  await box.fill(prefix)
  return page.getByTestId('slash-suggestions')
}

test('a command that needs a permission is offered only to members who hold it', async ({ browser }) => {
  const ownerCtx = await browser.newContext({ viewport: VIEWPORT })
  const memberCtx = await browser.newContext({ viewport: VIEWPORT })
  const botCtx = await browser.newContext()
  const owner = await ownerCtx.newPage()
  const member = await memberCtx.newPage()
  const botPage = await botCtx.newPage()
  try {
    await signIn(owner, ADMIN_USER, ADMIN_PASSWORD)
    const channelId = responseString(await apiFetch(owner, 'POST', '/api/admin/channels', { name: CHANNEL, type: 'text' }), 'id')
    await member.goto('/')
    if (suiteMember.registered) {
      expect((await apiFetch(member, 'POST', '/api/auth/login', { username: suiteMember.username, password: suiteMember.password })).status).toBe(200)
    } else {
      const invite = await apiFetch(owner, 'POST', '/api/admin/invites', { max_uses: 1 })
      expect((await apiFetch(member, 'POST', '/api/auth/register', { username: suiteMember.username, password: suiteMember.password, invite_code: responseString(invite, 'code') })).status).toBe(201)
      suiteMember.registered = true
    }

    const created = await apiFetch(owner, 'POST', '/api/admin/bots', { name: `Perm ${suffix}`, allowed_intents: 512 })
    expect(created.status).toBe(201)
    const botId = responseString(created, 'id')
    // The bot's role may view and write in the channel; commands of a bot that cannot are not offered.
    const roleId = responseString(await apiFetch(owner, 'POST', '/api/admin/roles', { name: `Perm role ${suffix}`, permissions: SEND_MESSAGES }), 'id')
    expect((await apiFetch(owner, 'PUT', `/api/admin/channels/${channelId}/overwrites`, { overwrites: [{ type: 'role', id: roleId, allow: VIEW_CHANNEL, deny: 0 }] })).status).toBe(204)
    expect((await apiFetch(owner, 'PUT', `/api/admin/bots/${botId}/roles`, { role_ids: [roleId] })).status).toBe(200)
    const token = responseString(await apiFetch(owner, 'POST', `/api/admin/bots/${botId}/tokens`, {}), 'secret')
    await botPage.goto('/')
    expect(await registerCommand(botPage, token, { name: OPEN_COMMAND, description: 'Open to everyone' })).toBe(200)
    expect(await registerCommand(botPage, token, { name: MOD_COMMAND, description: 'Only for moderators', default_member_permissions: ['ban_members'] })).toBe(200)

    await owner.goto(`/c/${CHANNEL}`)
    await member.goto(`/c/${CHANNEL}`)

    // The list shows the first few commands for a bare "/", so each one is looked up by name. The
    // restricted one is asked for after the open one has shown, so the list is known to be loaded.
    const memberList = await suggestionsFor(member, `/${OPEN_COMMAND}`)
    await expect(memberList).toContainText(`/${OPEN_COMMAND}`)
    await suggestionsFor(member, `/${MOD_COMMAND}`)
    await expect(member.getByTestId('slash-suggestions')).toHaveCount(0)

    // The Owner holds every permission and finds both.
    await expect(await suggestionsFor(owner, `/${OPEN_COMMAND}`)).toContainText(`/${OPEN_COMMAND}`)
    await expect(await suggestionsFor(owner, `/${MOD_COMMAND}`)).toContainText(`/${MOD_COMMAND}`)

    // The API agrees with the composer.
    const commandsFor = async (page: Page) => (await apiFetch(page, 'GET', `/api/channels/${channelId}/commands`)).json as { name: string }[]
    expect((await commandsFor(member)).map(c => c.name)).toContain(OPEN_COMMAND)
    expect((await commandsFor(member)).map(c => c.name)).not.toContain(MOD_COMMAND)
    expect((await commandsFor(owner)).map(c => c.name)).toContain(MOD_COMMAND)

    // Clearing the field on the bot's side opens the command to the member, who sees it after a reload.
    expect(await registerCommand(botPage, token, { name: MOD_COMMAND, description: 'Only for moderators', default_member_permissions: null })).toBe(200)
    await member.reload()
    await expect(await suggestionsFor(member, `/${MOD_COMMAND}`)).toContainText(`/${MOD_COMMAND}`)
    expect((await apiFetch(owner, 'DELETE', `/api/admin/bots/${botId}`)).status).toBe(204)
  } finally {
    await ownerCtx.close()
    await memberCtx.close()
    await botCtx.close()
  }
})
