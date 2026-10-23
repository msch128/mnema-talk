import { test, expect } from './fixtures'
import type { Page } from '@playwright/test'
import { apiFetch, responseString, responseUserId, suiteMember } from './helpers'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'Herzog'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
const OPEN = `such-${suffix}`
const HIDDEN = `heimlich-${suffix}`
const OLD_WORD = `uralt${suffix}`
const HIDDEN_WORD = `verborgen${suffix}`
const VIEW_CHANNEL = 1 << 1

async function signIn(page: Page, username: string, password: string) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('Benutzername').fill(username)
  await dialog.getByLabel('Passwort').fill(password)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

async function post(page: Page, channelId: string, content: string) {
  const res = await apiFetch(page, 'POST', `/api/channels/${channelId}/messages`, { content })
  expect(res.status).toBe(201)
  return responseString(res, 'id')
}

test('search filters, results and jumping to an old message', async ({ browser }) => {
  test.setTimeout(240_000)
  const aCtx = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  const bCtx = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  const a = await aCtx.newPage()
  const b = await bCtx.newPage()
  try {
    await signIn(a, ADMIN_USER, ADMIN_PASSWORD)
    const open = responseString(await apiFetch(a, 'POST', '/api/admin/channels', { name: OPEN, type: 'text' }), 'id')
    const hidden = responseString(await apiFetch(a, 'POST', '/api/admin/channels', { name: HIDDEN, type: 'text' }), 'id')
    const roles = await apiFetch(a, 'GET', '/api/roles')
    const list = (roles.json as { roles?: { id: string; is_everyone: boolean }[] }).roles ?? (roles.json as { id: string; is_everyone: boolean }[])
    const everyoneId = list.find(r => r.is_everyone)?.id
    expect((await apiFetch(a, 'PUT', `/api/admin/channels/${hidden}/overwrites`, { overwrites: [{ id: everyoneId, type: 'role', allow: 0, deny: VIEW_CHANNEL }] })).status).toBe(204)

    await b.goto('/')
    if (suiteMember.registered) {
      expect((await apiFetch(b, 'POST', '/api/auth/login', { username: suiteMember.username, password: suiteMember.password })).status).toBe(200)
    } else {
      const invite = await apiFetch(a, 'POST', '/api/admin/invites', { max_uses: 1 })
      expect((await apiFetch(b, 'POST', '/api/auth/register', { username: suiteMember.username, password: suiteMember.password, invite_code: responseString(invite, 'code') })).status).toBe(201)
      suiteMember.registered = true
    }
    const memberId = responseUserId(await apiFetch(b, 'GET', '/api/auth/me'))

    // An old message that mentions the member, buried under later history, plus a hidden one.
    await post(a, open, `${OLD_WORD} Anfang @${suiteMember.username}`)
    await post(a, open, `${OLD_WORD}-danach direkt dahinter`)
    for (let i = 0; i < 70; i++) await post(a, open, `fuell ${suffix} ${i}`)
    await post(a, hidden, `${HIDDEN_WORD} geheim @${suiteMember.username}`)
    await a.reload()

    // "Search in this channel" starts the query with an in: chip.
    await a.locator('[data-channel-type="text"]', { hasText: OPEN }).click()
    await a.getByTestId('search-in-channel').click()
    const dialog = a.getByRole('dialog')
    await expect(dialog.locator('[data-search-chip]')).toContainText(`in:${OPEN}`)
    await dialog.getByRole('combobox').fill(OLD_WORD)
    await expect(dialog.locator('[role="option"]')).toHaveCount(2)
    // The match is highlighted as text, in the hit.
    await expect(dialog.locator('#search-result-0 mark').first()).toContainText(OLD_WORD)

    // Typing a filter: from: autocompletes members and becomes a chip.
    // The whole name and a space turn the filter into a chip. A prefix would
    // depend on the suggestion list, which is capped and shared with every
    // member other specs have added to the database.
    await dialog.getByRole('combobox').fill(`${OLD_WORD} mentions:${suiteMember.username} `)
    await expect(dialog.locator('[data-search-chip]')).toHaveCount(2)
    await expect(dialog.locator('[role="option"]')).toHaveCount(1)

    // Jump to the old message: the surrounding history loads with it.
    await dialog.locator('#search-result-0').click()
    await expect(a).toHaveURL(new RegExp(`/c/${open}/m/`))
    await expect(a.getByText(`${OLD_WORD}-danach direkt dahinter`, { exact: true })).toBeVisible()

    // Date filters: nothing before the year 2000, everything after it.
    await a.keyboard.press('Control+k')
    const search = a.getByRole('dialog')
    // The last query is kept for the session: start from a clean line.
    while (await search.locator('[data-search-chip]').count()) await search.locator('[data-search-chip] button').first().click()
    await search.getByRole('combobox').fill(`${OLD_WORD} in:${OPEN} before:2000-01-01 `)
    await expect(search.getByText('Keine Nachrichten gefunden')).toBeVisible()
    await search.getByRole('button', { name: 'Filter before entfernen' }).click()
    await search.getByRole('combobox').fill(`${OLD_WORD} after:2000-01-01 `)
    await expect(search.locator('[role="option"]')).toHaveCount(2)
    await a.keyboard.press('Escape')

    // The member sees mentions of themself, but nothing from the hidden channel.
    await b.reload()
    await expect(b.getByTestId('search-in-channel')).toBeVisible()
    await b.keyboard.press('Control+k')
    const mine = b.getByRole('dialog')
    await mine.getByRole('combobox').fill(`mentions:${suiteMember.username} `)
    await expect(mine.locator('[role="option"]').first()).toBeVisible()
    await expect(mine.getByText(HIDDEN_WORD)).toHaveCount(0)
    await mine.getByRole('combobox').fill(HIDDEN_WORD)
    await expect(mine.getByText('Keine Nachrichten gefunden')).toBeVisible()
    expect(memberId).toBeTruthy()
  } finally {
    await aCtx.close()
    await bCtx.close()
  }
})
