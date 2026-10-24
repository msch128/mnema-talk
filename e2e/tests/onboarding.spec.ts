import { test, expect, showWelcomeFlow } from './fixtures'
import type { BrowserContext, Page } from '@playwright/test'
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

async function ensureSuiteMember(admin: Page, member: Page) {
  await member.goto('/')
  if (suiteMember.registered) {
    expect((await apiFetch(member, 'POST', '/api/auth/login', { username: suiteMember.username, password: suiteMember.password })).status).toBe(200)
    return
  }
  const invite = await apiFetch(admin, 'POST', '/api/admin/invites', { max_uses: 1 })
  expect((await apiFetch(member, 'POST', '/api/auth/register', { username: suiteMember.username, password: suiteMember.password, invite_code: responseString(invite, 'code') })).status).toBe(201)
  suiteMember.registered = true
}

// The same member, once more as a brand-new one: the welcome flow walks them
// through every step, and a raised rules version asks only for the rules.
async function welcomeFlowPhase(a: Page, b: Page, bCtx: BrowserContext, channelId: string) {
  // Headless Chromium reports notifications as blocked; a real new member is still undecided.
  await bCtx.addInitScript(() => Object.defineProperty(Notification, 'permission', { get: () => 'default', configurable: true }))
  // Other specs hide the flow so it cannot cover their pages; this one wants it.
  await showWelcomeFlow(bCtx)
  expect((await apiFetch(b, 'PUT', '/api/onboarding/tips', { welcome_done: false })).status).toBe(200)
  expect((await apiFetch(a, 'PUT', '/api/admin/onboarding', {
    community_name: COMMUNITY, welcome_text: 'Glad you are here.', rules_text: '# Rules\nBe kind.', start_channel_id: channelId, require_reacceptance: true,
  })).status).toBe(200)

  await b.goto('/')
  const dialog = b.getByRole('dialog')
  await expect(dialog.getByRole('heading', { name: `Willkommen bei ${COMMUNITY}` })).toBeVisible()
  await expect(dialog).toContainText('Glad you are here.')
  await dialog.getByRole('button', { name: 'Weiter' }).click()

  // The rules cannot be skipped or closed; reading works, writing waits for the click.
  await expect(dialog.getByRole('heading', { name: 'Die Regeln' })).toBeVisible()
  await expect(dialog.getByTestId('welcome-rules')).toContainText('Be kind.')
  await b.keyboard.press('Escape')
  await expect(dialog.getByRole('heading', { name: 'Die Regeln' })).toBeVisible()
  await expect(dialog.getByRole('button', { name: 'Einführung überspringen' })).toHaveCount(0)
  await dialog.getByRole('button', { name: 'Ich akzeptiere die Regeln' }).click()

  await expect(dialog.getByRole('heading', { name: 'Dein Profil' })).toBeVisible()
  // Not the display name: later specs find this member by the user name.
  await dialog.getByLabel(/^Bio/).fill('Neu hier.')
  await dialog.getByTestId('welcome-profile-save').click()
  await expect(dialog.getByRole('heading', { name: 'Immer auf dem Laufenden' })).toBeVisible()
  await dialog.getByRole('button', { name: 'Nicht jetzt' }).click()

  await expect(dialog).toBeHidden()
  await expect(b).toHaveURL(new RegExp(`/c/${channelId}$`))
  expect((await apiFetch(b, 'GET', '/api/onboarding')).json).toMatchObject({ rules_pending: false })

  // Done is stored on the account: a reload shows nothing.
  await b.reload()
  await expect(b.getByTestId('welcome-title')).toHaveCount(0)

  // The Owner raises the rules version: only the rules come back, and block.
  expect((await apiFetch(a, 'PUT', '/api/admin/onboarding', {
    community_name: COMMUNITY, welcome_text: 'Glad you are here.', rules_text: '# Rules\nBe kinder.', start_channel_id: channelId, require_reacceptance: true,
  })).status).toBe(200)
  await b.reload()
  await expect(dialog.getByRole('heading', { name: 'Die Regeln' })).toBeVisible()
  await expect(dialog.getByTestId('welcome-progress')).toHaveText('Schritt 1 von 1')
  await expect(dialog.getByTestId('welcome-rules')).toContainText('Be kinder.')
  await dialog.getByRole('button', { name: 'Ich akzeptiere die Regeln' }).click()
  await expect(dialog).toBeHidden()
  expect((await apiFetch(b, 'POST', `/api/channels/${channelId}/messages`, { content: 'angekommen' })).status).toBe(201)
}

test('invite links name the community, rules block a member until accepted, and a new member gets the welcome flow', async ({ browser }) => {
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
    await ensureSuiteMember(a, b)
    expect((await apiFetch(b, 'POST', `/api/channels/${channelId}/messages`, { content: 'before the rules' })).status).toBe(201)

    // Earlier runs against the same database may have raised the version already.
    const versionBefore = Number(((await apiFetch(b, 'GET', '/api/onboarding')).json as { rules_version?: number }).rules_version ?? 0)

    // The Owner writes the community name and rules in the admin console.
    await a.goto('/admin/onboarding')
    const form = a.getByTestId('onboarding-settings')
    await expect(form).toBeVisible()
    await form.getByLabel('Name der Community').fill(COMMUNITY)
    await form.getByLabel('Regeln').fill('# Rules\nBe kind.')
    await form.getByRole('button', { name: 'Vorschau' }).nth(1).click()
    await expect(form.getByTestId('rules-preview')).toContainText('Be kind.')
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
    expect(pending.json).toMatchObject({ rules_pending: true, community_name: COMMUNITY, rules_version: versionBefore + 1 })
    const refused = await apiFetch(b, 'POST', `/api/channels/${channelId}/messages`, { content: 'too early' })
    expect(refused.status).toBe(403)
    expect(refused.json).toMatchObject({ error: { code: 'RULES_NOT_ACCEPTED' } })
    expect((await apiFetch(b, 'GET', `/api/channels/${channelId}/messages`)).status).toBe(200)

    expect((await apiFetch(b, 'POST', '/api/onboarding/accept', { version: 1 })).status).toBe(204)
    expect((await apiFetch(b, 'POST', `/api/channels/${channelId}/messages`, { content: 'after the rules' })).status).toBe(201)
    // The Owner was never asked.
    expect((await apiFetch(a, 'POST', `/api/channels/${channelId}/messages`, { content: 'owner' })).status).toBe(201)

    await welcomeFlowPhase(a, b, bCtx, channelId)
  } finally {
    // Other specs post as this member: leave the instance without rules.
    await apiFetch(a, 'PUT', '/api/admin/onboarding', { community_name: '', welcome_text: '', rules_text: '', start_channel_id: null })
    await aCtx.close()
    await bCtx.close()
    await visitor.close()
  }
})
