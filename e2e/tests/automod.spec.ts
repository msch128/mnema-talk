import type { Page } from '@playwright/test'
import { test, expect } from './fixtures'
import { apiFetch, responseString, suiteMember } from './helpers'

// Automod in the admin console: the Owner adds a forbidden-words rule, a
// member's message with such a word is refused with the rule's name and never
// shows in the channel, the Owner's own message is not stopped, and with the
// rule switched off the same words go through. Every UI string is German (the
// suite's locale).

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'cool_guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
const CHANNEL = `automod-${suffix}`
const RULE = `No spoilers ${suffix}`
const WORD = `plotleak${suffix}`
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

async function say(page: Page, text: string) {
  const composer = page.getByRole('textbox', { name: `Nachricht an #${CHANNEL}` })
  await composer.fill(text)
  await composer.press('Enter')
}

test('a words rule blocks a member\'s message with its name, spares the Owner, and lets the words through once it is off', async ({ browser }) => {
  const ownerCtx = await browser.newContext({ viewport: VIEWPORT })
  const memberCtx = await browser.newContext({ viewport: VIEWPORT })
  const owner = await ownerCtx.newPage()
  const member = await memberCtx.newPage()
  try {
    await signIn(owner, ADMIN_USER, ADMIN_PASSWORD)
    expect((await apiFetch(owner, 'POST', '/api/admin/channels', { name: CHANNEL, type: 'text' })).status).toBe(201)
    await member.goto('/')
    if (suiteMember.registered) {
      expect((await apiFetch(member, 'POST', '/api/auth/login', { username: suiteMember.username, password: suiteMember.password })).status).toBe(200)
    } else {
      const invite = await apiFetch(owner, 'POST', '/api/admin/invites', { max_uses: 1 })
      expect((await apiFetch(member, 'POST', '/api/auth/register', { username: suiteMember.username, password: suiteMember.password, invite_code: responseString(invite, 'code') })).status).toBe(201)
      suiteMember.registered = true
    }

    // The Owner adds the rule in the admin console.
    await owner.goto('/admin/automod')
    await expect(owner.getByTestId('admin-automod')).toBeVisible()
    await owner.getByTestId('automod-add').locator('[data-add="words"]').click()
    const form = owner.getByTestId('automod-form')
    await form.getByTestId('am-name').fill(RULE)
    await form.getByTestId('am-words').fill(WORD)
    await form.getByTestId('unsaved-save').click()
    const row = owner.locator('[data-rule]', { hasText: RULE })
    await expect(row).toBeVisible()
    await expect(row.getByTestId('rule-summary')).toHaveText('Wörter und Wendungen: 1')

    // The member's message is refused and says which rule it broke; nothing reaches the channel.
    await member.goto(`/c/${CHANNEL}`)
    await say(member, `Do not read this: ${WORD}`)
    await expect(member.getByRole('alert').filter({ hasText: `gegen die Regel „${RULE}“` })).toBeVisible()
    await expect(member.locator('[data-msg-id]', { hasText: WORD })).toHaveCount(0)

    // The Owner is never stopped by a rule.
    await owner.goto(`/c/${CHANNEL}`)
    await say(owner, `Owner may say ${WORD}`)
    await expect(owner.locator('[data-msg-id]', { hasText: `Owner may say ${WORD}` })).toBeVisible()
    expect((await apiFetch(owner, 'GET', '/api/admin/automod')).status).toBe(200)

    // With the rule switched off, the member's words go through.
    await owner.goto('/admin/automod')
    await owner.locator('[data-rule]', { hasText: RULE }).getByTestId('rule-edit').click()
    const edit = owner.getByTestId('automod-form')
    await edit.getByTestId('am-enabled').click()
    await edit.getByTestId('unsaved-save').click()
    await expect(owner.locator('[data-rule]', { hasText: RULE }).getByTestId('rule-off')).toBeVisible()
    await say(member, `Now allowed ${WORD}`)
    await expect(member.locator('[data-msg-id]', { hasText: `Now allowed ${WORD}` })).toBeVisible()
  } finally {
    // Leave no rule behind, so the rule list of a second run against the same database starts clean.
    const rules = await apiFetch(owner, 'GET', '/api/admin/automod')
    const list = (rules.json as { rules?: { id: string; name: string }[] }).rules ?? []
    for (const rule of list.filter(r => r.name === RULE)) await apiFetch(owner, 'DELETE', `/api/admin/automod/rules/${rule.id}`)
    await ownerCtx.close()
    await memberCtx.close()
  }
})
