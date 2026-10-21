import { test, expect } from './fixtures'
import type { Page } from '@playwright/test'
import { apiFetch, responseString, suiteMember } from './helpers'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'Herzog'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const CHANNEL = `welcome-${Date.now().toString(36)}`
const COMMUNITY = `Heimat ${Date.now().toString(36)}`

async function signIn(page: Page, username: string, password: string) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByRole('heading', { name: 'Mnema Talk' })).toBeVisible()
  await dialog.getByLabel('Benutzername').fill(username)
  await dialog.getByLabel('Passwort').fill(password)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

test('invite links name the community, and rules must be accepted before a member can write', async ({ browser }) => {
  const aCtx = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  const bCtx = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  const visitor = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  const a = await aCtx.newPage()
  const b = await bCtx.newPage()
  const v = await visitor.newPage()
  try {
    await signIn(a, ADMIN_USER, ADMIN_PASSWORD)
    const channel = await apiFetch(a, 'POST', '/api/admin/channels', { name: CHANNEL, type: 'text' })
    expect(channel.status).toBe(201)
    const channelId = responseString(channel, 'id')

    // The member of this suite exists before any rules do.
    await b.goto('/')
    if (suiteMember.registered) {
      expect((await apiFetch(b, 'POST', '/api/auth/login', { username: suiteMember.username, password: suiteMember.password })).status).toBe(200)
    } else {
      const invite = await apiFetch(a, 'POST', '/api/admin/invites', { max_uses: 1 })
      expect((await apiFetch(b, 'POST', '/api/auth/register', { username: suiteMember.username, password: suiteMember.password, invite_code: responseString(invite, 'code') })).status).toBe(201)
      suiteMember.registered = true
    }
    expect((await apiFetch(b, 'POST', `/api/channels/${channelId}/messages`, { content: 'before the rules' })).status).toBe(201)

    // The Owner writes the community name and rules in the admin console.
    await a.goto('/admin/onboarding')
    const form = a.getByTestId('onboarding-settings')
    await expect(form).toBeVisible()
    await form.getByLabel('Name der Community').fill(COMMUNITY)
    await form.getByLabel('Regeln').fill('# Regeln\nSei nett.')
    await form.getByRole('button', { name: 'Vorschau' }).nth(1).click()
    await expect(form.getByTestId('rules-preview')).toContainText('Sei nett.')
    await form.getByTestId('onboarding-save').click()
    await expect(a.getByText('Willkommen und Regeln gespeichert.')).toBeVisible()

    // An invite link shows the community, fills in the code and leaks nothing else.
    const invite = await apiFetch(a, 'POST', '/api/admin/invites', { max_uses: 1, expires_in_hours: 24 })
    const code = responseString(invite, 'code')
    await v.goto(`/invite/${code}`)
    await expect(v.getByTestId('login-heading')).toHaveText(COMMUNITY)
    await expect(v.getByLabel('Einladungscode')).toHaveValue(code)
    await expect(v.getByTestId('invite-expiry')).toBeVisible()
    const preview = await apiFetch(v, 'GET', `/api/invites/${code}`)
    expect(Object.keys(preview.json as object).sort()).toEqual(['community_name', 'expires_at', 'valid'])
    await v.goto('/invite/zzzzzzzzzzzz')
    await expect(v.getByTestId('invite-invalid')).toBeVisible()
    await expect(v.getByTestId('login-heading')).toHaveText('Mnema Talk')
    expect((await apiFetch(v, 'GET', '/api/invites/zzzzzzzzzzzz')).json).toEqual({ valid: false })

    // The existing member must accept first: reading works, writing answers RULES_NOT_ACCEPTED.
    const pending = await apiFetch(b, 'GET', '/api/onboarding')
    expect(pending.json).toMatchObject({ rules_pending: true, community_name: COMMUNITY, rules_version: 1 })
    const refused = await apiFetch(b, 'POST', `/api/channels/${channelId}/messages`, { content: 'too early' })
    expect(refused.status).toBe(403)
    expect(refused.json).toMatchObject({ error: { code: 'RULES_NOT_ACCEPTED' } })
    expect((await apiFetch(b, 'GET', `/api/channels/${channelId}/messages`)).status).toBe(200)

    expect((await apiFetch(b, 'POST', '/api/onboarding/accept', { version: 1 })).status).toBe(204)
    expect((await apiFetch(b, 'POST', `/api/channels/${channelId}/messages`, { content: 'after the rules' })).status).toBe(201)
    // The Owner was never asked.
    expect((await apiFetch(a, 'POST', `/api/channels/${channelId}/messages`, { content: 'owner' })).status).toBe(201)
  } finally {
    // Other specs post as this member: leave the instance without rules.
    await apiFetch(a, 'PUT', '/api/admin/onboarding', { community_name: '', welcome_text: '', rules_text: '', start_channel_id: null })
    await aCtx.close()
    await bCtx.close()
    await visitor.close()
  }
})
