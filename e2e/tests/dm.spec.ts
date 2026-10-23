import { test, expect } from './fixtures'
import type { Page } from '@playwright/test'
import { apiFetch, responseString, responseUserId, suiteMember } from './helpers'

// Direct messages, blocking and reports in the web client (1.1). Owner and
// members sit in separate browser contexts; every UI string is German (the
// suite's locale).

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'Herzog'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
const VIEWPORT = { width: 1400, height: 900 }
const REFUSED = 'An dieses Mitglied kann keine Nachricht gesendet werden.'

async function signIn(page: Page, username: string, password: string) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByRole('heading', { name: 'Mnema Talk' })).toBeVisible()
  await dialog.getByLabel('Benutzername').fill(username)
  await dialog.getByLabel('Passwort').fill(password)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

/** The member the whole suite shares: registered on first use, signed in afterwards. */
async function joinSuiteMember(owner: Page, page: Page): Promise<string> {
  await page.goto('/')
  if (suiteMember.registered) {
    expect((await apiFetch(page, 'POST', '/api/auth/login', { username: suiteMember.username, password: suiteMember.password })).status).toBe(200)
  } else {
    const invite = await apiFetch(owner, 'POST', '/api/admin/invites', { max_uses: 1 })
    expect((await apiFetch(page, 'POST', '/api/auth/register', { username: suiteMember.username, password: suiteMember.password, invite_code: responseString(invite, 'code') })).status).toBe(201)
    suiteMember.registered = true
  }
  const id = responseUserId(await apiFetch(page, 'GET', '/api/auth/me'))
  await page.goto('/')
  return id
}

/** A member of this spec's own: invited by the Owner, signed in on this page. */
async function joinNewMember(owner: Page, page: Page, name: string): Promise<string> {
  await page.goto('/')
  const invite = await apiFetch(owner, 'POST', '/api/admin/invites', { max_uses: 1 })
  const registered = await apiFetch(page, 'POST', '/api/auth/register', { username: name, password: `pw-${suffix}-${name}-x1`, invite_code: responseString(invite, 'code') })
  expect(registered.status).toBe(201)
  const id = responseUserId(await apiFetch(page, 'GET', '/api/auth/me'))
  await page.goto('/')
  return id
}

const composerOf = (page: Page) => page.getByPlaceholder(/^Nachricht an /)
const rowOf = (page: Page, text: string | RegExp) => page.locator('[data-dm]', { hasText: text })

async function write(page: Page, text: string) {
  const composer = composerOf(page)
  await composer.fill(text)
  await composer.press('Enter')
}

/**
 * The conversation of two with this member. Starting one counts against the
 * server's three tries a minute and ten a day, so an existing one is reused.
 */
async function startDm(owner: Page, memberId: string): Promise<string> {
  const mine = (await apiFetch(owner, 'GET', '/api/dms')).json as { id: string; group: boolean; members: { id: string }[] }[]
  const existing = mine.find(dm => !dm.group && dm.members.some(m => m.id === memberId))
  if (existing) return existing.id
  const started = await apiFetch(owner, 'POST', '/api/dms', { user_ids: [memberId] })
  expect([200, 201]).toContain(started.status)
  return responseString(started, 'id')
}

test('two members start a direct message and write to each other', async ({ browser }) => {
  const ownerCtx = await browser.newContext({ viewport: VIEWPORT })
  const memberCtx = await browser.newContext({ viewport: VIEWPORT })
  const owner = await ownerCtx.newPage()
  const member = await memberCtx.newPage()
  try {
    await signIn(owner, ADMIN_USER, ADMIN_PASSWORD)
    await joinSuiteMember(owner, member)
    const health = await apiFetch(owner, 'GET', '/api/health')
    expect((health.json as { features?: string[] }).features).toEqual(expect.arrayContaining(['dm', 'blocks', 'reports']))

    // The Owner starts the conversation from the picker.
    await owner.getByTestId('dm-new').click()
    const picker = owner.getByTestId('dm-create')
    await picker.locator(`[data-dm-candidate="${suiteMember.username}"]`).click()
    await picker.getByTestId('dm-submit').click()
    await expect(owner).toHaveURL(/\/c\/[0-9a-f-]{36}$/)
    await expect(owner.getByTestId('chat-channel-name')).toHaveText(suiteMember.username)
    await write(owner, `hallo ${suffix}`)
    await expect(owner.getByText(`hallo ${suffix}`, { exact: true })).toBeVisible()

    // It reaches the member live (the socket asked for conversations), with a badge.
    await expect(member.locator('[data-testid="dm-unread"]').first()).toHaveText('1')
    await rowOf(member, ADMIN_USER).click()
    await expect(member.getByText(`hallo ${suffix}`, { exact: true })).toBeVisible()
    await expect(member.getByTestId('dm-unread')).toHaveCount(0)
    await write(member, `antwort ${suffix}`)
    await expect(owner.getByText(`antwort ${suffix}`, { exact: true })).toBeVisible()
    await expect(member.getByText(`antwort ${suffix}`, { exact: true })).toBeVisible()

    // A conversation of two cannot be left; the member list of the community is not shown beside it.
    await member.getByTestId('dm-info-button').click()
    const info = member.getByTestId('dm-info')
    await expect(info.getByTestId('dm-pair-hint')).toBeVisible()
    await expect(info.getByTestId('dm-leave')).toHaveCount(0)
    await member.keyboard.press('Escape')
    await expect(member.getByTestId('dm-info')).toHaveCount(0)
  } finally {
    await ownerCtx.close()
    await memberCtx.close()
  }
})

test('a block folds the messages and makes the composer refuse', async ({ browser }) => {
  const ownerCtx = await browser.newContext({ viewport: VIEWPORT })
  const memberCtx = await browser.newContext({ viewport: VIEWPORT })
  const owner = await ownerCtx.newPage()
  const member = await memberCtx.newPage()
  let ownerId = ''
  try {
    await signIn(owner, ADMIN_USER, ADMIN_PASSWORD)
    const memberId = await joinSuiteMember(owner, member)
    ownerId = responseUserId(await apiFetch(owner, 'GET', '/api/auth/me'))
    const dmId = await startDm(owner, memberId)
    const channel = await apiFetch(owner, 'POST', '/api/admin/channels', { name: `blk-${suffix}`, type: 'text' })
    expect(channel.status).toBe(201)
    const channelId = responseString(channel, 'id')
    expect((await apiFetch(owner, 'POST', `/api/channels/${dmId}/messages`, { content: `vor dem Block ${suffix}` })).status).toBe(201)
    expect((await apiFetch(owner, 'POST', `/api/channels/${channelId}/messages`, { content: `im Kanal ${suffix}` })).status).toBe(201)

    await member.goto(`/c/${dmId}`)
    await expect(member.getByText(`vor dem Block ${suffix}`, { exact: true })).toBeVisible()
    await member.getByTestId('dm-info-button').click()
    await member.getByTestId('dm-block').click()
    await member.keyboard.press('Escape')

    // Their messages fold away in the conversation and in channels; one can be shown again.
    await expect(member.getByTestId('blocked-message').first()).toBeVisible()
    await expect(member.getByText(`vor dem Block ${suffix}`, { exact: true })).toHaveCount(0)
    await member.goto(`/c/${channelId}`)
    await expect(member.getByTestId('blocked-message')).toHaveCount(1)
    await expect(member.getByText(`im Kanal ${suffix}`, { exact: true })).toHaveCount(0)
    await member.getByTestId('blocked-message-show').click()
    await expect(member.getByText(`im Kanal ${suffix}`, { exact: true })).toBeVisible()

    // Both sides are refused with the same calm sentence; nothing arrives.
    await owner.goto(`/c/${dmId}`)
    await write(owner, `nach dem Block ${suffix}`)
    await expect(owner.getByText(REFUSED)).toBeVisible()
    await member.goto(`/c/${dmId}`)
    await write(member, `ich auch ${suffix}`)
    await expect(member.getByText(REFUSED)).toBeVisible()
    await expect(owner.getByText(`ich auch ${suffix}`, { exact: true })).toHaveCount(0)
    await expect(member.getByText(`nach dem Block ${suffix}`, { exact: true })).toHaveCount(0)

    // Lifting the block (from the conversation's info) makes the conversation work again.
    await member.getByTestId('dm-info-button').click()
    await member.getByTestId('dm-unblock').click()
    await member.keyboard.press('Escape')
    await write(member, `wieder da ${suffix}`)
    await expect(owner.getByText(`wieder da ${suffix}`, { exact: true })).toBeVisible()
    await expect(member.getByText(`vor dem Block ${suffix}`, { exact: true })).toBeVisible()
  } finally {
    if (ownerId) await apiFetch(member, 'DELETE', `/api/me/blocks/${ownerId}`)
    await ownerCtx.close()
    await memberCtx.close()
  }
})

test('a group takes a member who reads its history, and a member can leave it', async ({ browser }) => {
  const names = ['b', 'c'].map(n => `dm${n}${suffix}`)
  const contexts = await Promise.all(Array.from({ length: 4 }, () => browser.newContext({ viewport: VIEWPORT })))
  const [owner, first, second, third] = (await Promise.all(contexts.map(c => c.newPage()))) as [Page, Page, Page, Page]
  try {
    await signIn(owner, ADMIN_USER, ADMIN_PASSWORD)
    await joinSuiteMember(owner, first)
    await joinNewMember(owner, second, names[0] as string)
    await joinNewMember(owner, third, names[1] as string)
    const secondName = names[0] as string

    // The Owner picks two members: that is a group.
    await owner.getByTestId('dm-new').click()
    const picker = owner.getByTestId('dm-create')
    await picker.locator(`[data-dm-candidate="${suiteMember.username}"]`).click()
    await picker.locator(`[data-dm-candidate="${secondName}"]`).click()
    await expect(picker.getByTestId('dm-submit')).toHaveText('Gruppengespräch beginnen')
    await picker.getByTestId('dm-submit').click()
    await expect(owner).toHaveURL(/\/c\/[0-9a-f-]{36}$/)
    await expect(owner.getByTestId('chat-channel-name')).toContainText(secondName)
    await write(owner, `willkommen ${suffix}`)
    await expect(owner.getByText(`willkommen ${suffix}`, { exact: true })).toBeVisible()
    await expect(rowOf(first, secondName)).toBeVisible()
    await expect(rowOf(second, suiteMember.username)).toBeVisible()

    // A fourth person is added and reads what was written before.
    await owner.getByTestId('dm-info-button').click()
    await owner.getByTestId('dm-add-member').click()
    await owner.locator(`[data-dm-candidate="${names[1]}"]`).click()
    await owner.getByTestId('dm-submit').click()
    await expect(owner.getByTestId('dm-create')).toHaveCount(0)
    await owner.keyboard.press('Escape')
    await expect(rowOf(third, secondName)).toBeVisible()
    await rowOf(third, secondName).click()
    await expect(third.getByText(`willkommen ${suffix}`, { exact: true })).toBeVisible()

    // A member leaves: the group disappears for them and shrinks for the others.
    await rowOf(second, suiteMember.username).click()
    const groupUrl = second.url()
    await second.getByTestId('dm-info-button').click()
    await second.getByTestId('dm-leave').click()
    await second.getByRole('alertdialog').getByRole('button', { name: 'Gespräch verlassen' }).click()
    await expect(second.locator('[data-dm]', { hasText: suiteMember.username })).toHaveCount(0)
    await expect(second).not.toHaveURL(groupUrl)
    await expect(owner.getByTestId('chat-channel-name')).not.toContainText(secondName)
    await expect(owner.getByTestId('chat-channel-name')).toContainText(suiteMember.username)
  } finally {
    await Promise.all(contexts.map(c => c.close()))
  }
})

test('the Owner switches direct messages off and on again', async ({ browser }) => {
  const ownerCtx = await browser.newContext({ viewport: VIEWPORT })
  const memberCtx = await browser.newContext({ viewport: VIEWPORT })
  const owner = await ownerCtx.newPage()
  const member = await memberCtx.newPage()
  try {
    await signIn(owner, ADMIN_USER, ADMIN_PASSWORD)
    const memberId = await joinSuiteMember(owner, member)
    const dmId = await startDm(owner, memberId)
    expect((await apiFetch(owner, 'POST', `/api/channels/${dmId}/messages`, { content: `lesbar ${suffix}` })).status).toBe(201)
    await member.goto(`/c/${dmId}`)
    const composer = member.locator('textarea')
    await expect(composer).toBeEnabled()
    await expect(member.getByTestId('dm-new')).toBeVisible()

    // Off: nobody can start or write, the member sees it without reloading, history stays readable.
    await owner.goto('/admin/onboarding')
    await owner.getByTestId('dm-enabled').uncheck()
    await owner.getByTestId('onboarding-save').click()
    await expect(owner.getByTestId('dm-enabled')).not.toBeChecked()
    await expect(composer).toBeDisabled()
    await expect(composer).toHaveAttribute('placeholder', 'Du kannst gerade keine Direktnachrichten schreiben.')
    await expect(member.getByTestId('dm-new')).toHaveCount(0)
    await expect(member.getByText(`lesbar ${suffix}`, { exact: true })).toBeVisible()

    // On again.
    await owner.getByTestId('dm-enabled').check()
    await owner.getByTestId('onboarding-save').click()
    await expect(composer).toBeEnabled()
    await expect(member.getByTestId('dm-new')).toBeVisible()
  } finally {
    const settings = await apiFetch(owner, 'GET', '/api/admin/onboarding')
    if (settings.status === 200) {
      const s = settings.json as { community_name: string; welcome_text: string; rules_text: string; start_channel_id: string | null }
      await apiFetch(owner, 'PUT', '/api/admin/onboarding', { community_name: s.community_name, welcome_text: s.welcome_text, rules_text: s.rules_text, start_channel_id: s.start_channel_id, require_reacceptance: false, direct_messages_enabled: true })
    }
    await ownerCtx.close()
    await memberCtx.close()
  }
})

test('a reported direct message reaches the moderators, who read the copy and resolve it', async ({ browser }) => {
  const ownerCtx = await browser.newContext({ viewport: VIEWPORT })
  const memberCtx = await browser.newContext({ viewport: VIEWPORT })
  const owner = await ownerCtx.newPage()
  const member = await memberCtx.newPage()
  try {
    await signIn(owner, ADMIN_USER, ADMIN_PASSWORD)
    const memberId = await joinSuiteMember(owner, member)
    const dmId = await startDm(owner, memberId)
    const text = `unerwünscht ${suffix}`
    expect((await apiFetch(member, 'POST', `/api/channels/${dmId}/messages`, { content: text })).status).toBe(201)

    // The Owner reports it from the message menu.
    await owner.goto(`/c/${dmId}`)
    await owner.getByText(text, { exact: true }).click({ button: 'right' })
    await owner.getByRole('menuitem', { name: 'Nachricht melden' }).click()
    const dialog = owner.getByTestId('report-dialog')
    await dialog.locator('[data-report-category="harassment"]').check()
    await dialog.getByTestId('report-note').fill('Test-Meldung')
    await dialog.getByTestId('report-submit').click()
    await expect(owner.getByText('Meldung gesendet. Die Moderatoren sehen sie sich an.')).toBeVisible()
    await expect(owner.getByTestId('report-dialog')).toHaveCount(0)

    // It is in Admin > Meldungen; the copy is readable although the conversation is private.
    await owner.goto('/admin/reports')
    const row = owner.locator('[data-report]', { hasText: 'Direktnachricht' }).filter({ hasText: 'Test-Meldung' })
    await expect(row).toBeVisible()
    await row.click()
    await expect(owner.getByTestId('report-context')).toContainText(text)
    await owner.getByTestId('report-resolution').fill('geprüft')
    await owner.getByTestId('report-resolve').click()
    await expect(owner.getByText('Meldung als erledigt markiert.')).toBeVisible()
    await expect(owner.locator('[data-report]', { hasText: 'Test-Meldung' })).toHaveCount(0)

    // Closed reports are listed under "Erledigt" and can be deleted.
    await owner.locator('[data-filter="resolved"]').click()
    const closed = owner.locator('[data-report]', { hasText: 'Test-Meldung' })
    await expect(closed).toBeVisible()
    await closed.click()
    await expect(owner.getByTestId('report-closed')).toContainText('Erledigt von')
    await owner.getByTestId('report-delete').click()
    await owner.getByRole('alertdialog').getByRole('button', { name: 'Meldung löschen' }).click()
    await expect(owner.locator('[data-report]', { hasText: 'Test-Meldung' })).toHaveCount(0)
  } finally {
    await ownerCtx.close()
    await memberCtx.close()
  }
})
