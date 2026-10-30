import type { Page } from '@playwright/test'
import { apiFetch, awaitApiBudget, responseString, suiteMember, expectTalkLive, leaveTalks, trackPeerConnections } from './helpers'
import { test, expect } from './fixtures'

// Live reactions of a Talk: every quick reaction, and the full emoji picker
// ("more") with its search, for standard and custom emoji, each shown to the
// other member. Needs a call (fake microphone), so it runs in CI only.

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'cool_guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
const TALK = `Reactions ${suffix}`
const EMOJI = `party_${suffix}`
const QUICK = ['👍', '❤️', '😂', '🔥', '🎉', '🚀']

async function signIn(page: Page, username: string, password: string) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('Benutzername').fill(username)
  await dialog.getByLabel('Passwort').fill(password)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

// A 32x32 PNG drawn in the page, uploaded as a custom emoji of the community.
async function uploadEmoji(page: Page, name: string): Promise<number> {
  return page.evaluate(async name => {
    const canvas = document.createElement('canvas')
    canvas.width = canvas.height = 32
    const ctx = canvas.getContext('2d')
    if (ctx) { ctx.fillStyle = '#e91e63'; ctx.fillRect(0, 0, 32, 32) }
    const blob = await new Promise<Blob | null>(resolve => canvas.toBlob(resolve, 'image/png'))
    if (!blob) return 0
    const form = new FormData()
    form.append('name', name)
    form.append('file', blob, `${name}.png`)
    return (await fetch('/api/admin/emoji', { method: 'POST', body: form, credentials: 'same-origin' })).status
  }, name)
}

test('every quick reaction and the picker reach the other member of a Talk', async ({ browser }) => {
  test.setTimeout(240_000)
  const ctxOptions = { permissions: ['microphone' as const], viewport: { width: 1400, height: 900 } }
  const aCtx = await browser.newContext(ctxOptions)
  const bCtx = await browser.newContext(ctxOptions)
  await aCtx.addInitScript(trackPeerConnections)
  await bCtx.addInitScript(trackPeerConnections)
  const a = await aCtx.newPage()
  const b = await bCtx.newPage()

  await signIn(a, ADMIN_USER, ADMIN_PASSWORD)
  if (await awaitApiBudget(a)) await a.reload()
  expect(await uploadEmoji(a, EMOJI)).toBe(201)
  const created = await apiFetch(a, 'POST', '/api/admin/channels', { name: TALK, type: 'voice' })
  expect(created.status).toBe(201)
  const talkId = responseString(created, 'id')

  await b.goto('/')
  if (!suiteMember.registered) {
    const invite = await apiFetch(a, 'POST', '/api/admin/invites', { max_uses: 1 })
    expect((await apiFetch(b, 'POST', '/api/auth/register', { username: suiteMember.username, password: suiteMember.password, invite_code: responseString(invite, 'code') })).status).toBe(201)
    suiteMember.registered = true
  } else {
    expect((await apiFetch(b, 'POST', '/api/auth/login', { username: suiteMember.username, password: suiteMember.password })).status).toBe(200)
  }
  await b.reload()

  const row = (page: Page) => page.locator('[data-channel-type="voice"]', { hasText: TALK })
  await row(a).click()
  await expectTalkLive(a)
  await row(b).click()
  await expectTalkLive(b)

  const floating = (page: Page, text: string) => page.getByTestId('talk-reaction').filter({ hasText: text })

  // Quick reactions: the allowance is five per ten seconds, so the two members take turns.
  for (const [i, emoji] of QUICK.entries()) {
    const [from, to] = i % 2 === 0 ? [b, a] : [a, b]
    await from.getByTestId('talk-react').click()
    await from.getByTestId('reaction-palette').getByRole('button', { name: emoji, exact: true }).click()
    await expect(floating(to, emoji).first(), `quick reaction ${emoji}`).toBeVisible()
  }

  // The full picker: search, then a standard emoji.
  await b.getByTestId('talk-react').click()
  await b.getByTestId('reaction-more').click()
  const picker = b.getByTestId('emoji-picker').locator('emoji-picker')
  await picker.locator('input[type="search"]').fill('partying')
  await expect(picker.getByRole('option').first()).toBeVisible()
  // The footer names the emoji under the pointer; Enter in the search picks the first result.
  await picker.getByRole('option').first().hover()
  await expect(b.getByTestId('emoji-preview-code')).toHaveText(':partying_face:')
  await picker.locator('input[type="search"]').press('Enter')
  await expect(floating(a, '🥳').first()).toBeVisible()

  // The same picker lists the community's custom emoji; one is found by name and shown as its picture.
  await b.getByTestId('talk-react').click()
  await b.getByTestId('reaction-more').click()
  const custom = b.getByTestId('emoji-picker').locator('emoji-picker')
  await custom.locator('input[type="search"]').fill(EMOJI)
  await expect(custom.getByRole('option').first()).toBeVisible()
  await custom.getByRole('option').first().hover()
  await expect(b.getByTestId('emoji-preview-code')).toHaveText(`:${EMOJI}:`)
  await custom.getByRole('option').first().click()
  await expect(a.getByTestId('talk-reaction').locator('img').first()).toBeVisible()
  await expect(b.getByTestId('talk-reaction').locator('img').first()).toBeVisible()

  // Escape clears the search first and closes the picker second.
  await b.getByTestId('talk-react').click()
  await b.getByTestId('reaction-more').click()
  const last = b.getByTestId('emoji-picker').locator('emoji-picker').locator('input[type="search"]')
  await last.fill('tada')
  await b.keyboard.press('Escape')
  await expect(last).toHaveValue('')
  await b.keyboard.press('Escape')
  await expect(b.getByTestId('emoji-picker')).toHaveCount(0)

  await leaveTalks([a, b])
  await apiFetch(a, 'DELETE', `/api/admin/channels/${talkId}`)
  const list = await apiFetch(a, 'GET', '/api/emoji')
  const emoji = (list.json as { emoji?: { id: string; name: string }[] } | null)?.emoji?.find(e => e.name === EMOJI)
  if (emoji) await apiFetch(a, 'DELETE', `/api/admin/emoji/${emoji.id}`)
  await aCtx.close()
  await bCtx.close()
})
