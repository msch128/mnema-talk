import type { Page } from '@playwright/test'
import { test, expect } from './fixtures'
import { apiFetch, responseString, suiteMember } from './helpers'

// Threads on a wide screen: a reply goes to the thread and not to the channel,
// the root counts it for everyone at once, a second member answers from the
// counter, and following and closing work. Every UI string is German (the
// suite's locale).

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'cool_guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
const CHANNEL = `threads-${suffix}`
const ROOT = `Thread root ${suffix}`
const OWNER_REPLY = `Owner reply ${suffix}`
const MEMBER_REPLY = `Member reply ${suffix}`
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

/** The member the whole suite shares: registered on first use, signed in afterwards. */
async function joinSuiteMember(owner: Page, page: Page) {
  await page.goto('/')
  if (suiteMember.registered) {
    expect((await apiFetch(page, 'POST', '/api/auth/login', { username: suiteMember.username, password: suiteMember.password })).status).toBe(200)
    return
  }
  const invite = await apiFetch(owner, 'POST', '/api/admin/invites', { max_uses: 1 })
  expect((await apiFetch(page, 'POST', '/api/auth/register', { username: suiteMember.username, password: suiteMember.password, invite_code: responseString(invite, 'code') })).status).toBe(201)
  suiteMember.registered = true
}

const threadOf = (page: Page) => page.locator('aside').filter({ has: page.getByTestId('thread-close') })

async function reply(page: Page, text: string) {
  const composer = threadOf(page).getByPlaceholder('Im Thread antworten …')
  await composer.fill(text)
  await composer.press('Enter')
}

test('a reply goes to the thread, the root counts it for everyone, and the thread can be followed and closed', async ({ browser }) => {
  const ownerCtx = await browser.newContext({ viewport: VIEWPORT })
  const memberCtx = await browser.newContext({ viewport: VIEWPORT })
  const owner = await ownerCtx.newPage()
  const member = await memberCtx.newPage()
  try {
    await signIn(owner, ADMIN_USER, ADMIN_PASSWORD)
    const channelId = responseString(await apiFetch(owner, 'POST', '/api/admin/channels', { name: CHANNEL, type: 'text' }), 'id')
    await joinSuiteMember(owner, member)
    expect((await apiFetch(owner, 'POST', `/api/channels/${channelId}/messages`, { content: ROOT })).status).toBe(201)

    await owner.goto(`/c/${CHANNEL}`)
    await member.goto(`/c/${CHANNEL}`)
    const ownerRoot = owner.locator('[data-msg-id]', { hasText: ROOT })
    const memberRoot = member.locator('[data-msg-id]', { hasText: ROOT })
    await expect(ownerRoot).toBeVisible()
    await expect(memberRoot).toBeVisible()

    // The Owner opens the thread of the root and answers; the answer stays out of the channel.
    await ownerRoot.hover()
    await ownerRoot.getByTestId('msg-open-thread').click()
    const ownerThread = threadOf(owner)
    await expect(ownerThread).toContainText('Noch keine Antworten')
    await reply(owner, OWNER_REPLY)
    await expect(ownerThread).toContainText(OWNER_REPLY)
    await expect(ownerThread).toContainText('1 Antwort')
    await expect(owner.locator('[data-msg-id]', { hasText: OWNER_REPLY })).toHaveCount(0)

    // The member sees the count on the root without a reload, opens the thread from it and answers.
    await expect(memberRoot.getByRole('button', { name: /1 Antwort/ })).toBeVisible()
    await memberRoot.getByRole('button', { name: /1 Antwort/ }).click()
    const memberThread = threadOf(member)
    await expect(memberThread).toContainText(OWNER_REPLY)
    await reply(member, MEMBER_REPLY)
    await expect(memberThread).toContainText(MEMBER_REPLY)

    // The Owner's open thread takes the member's reply live, and the root now counts two.
    await expect(ownerThread).toContainText(MEMBER_REPLY)
    await expect(ownerThread).toContainText('2 Antworten')
    await expect(ownerRoot.getByRole('button', { name: /2 Antworten/ })).toBeVisible()

    // Following is a switch that says what it does; closing hides the panel and the count stays.
    const follow = ownerThread.getByTestId('thread-follow')
    const before = await follow.getAttribute('aria-pressed')
    await follow.click()
    await expect(follow).toHaveAttribute('aria-pressed', before === 'true' ? 'false' : 'true')
    await ownerThread.getByTestId('thread-close').click()
    await expect(ownerThread).toBeHidden()
    await expect(ownerRoot.getByRole('button', { name: /2 Antworten/ })).toBeVisible()
  } finally {
    await ownerCtx.close()
    await memberCtx.close()
  }
})
