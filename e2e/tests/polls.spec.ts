import { test, expect } from './fixtures'
import type { Page } from '@playwright/test'
import { apiFetch, responseString, suiteMember } from './helpers'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'Herzog'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const CHANNEL = `polls-${Date.now().toString(36)}`

async function signIn(page: Page, username: string, password: string) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByRole('heading', { name: 'Mnema Talk' })).toBeVisible()
  await dialog.getByLabel('Benutzername').fill(username)
  await dialog.getByLabel('Passwort').fill(password)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

test('a poll from the slash menu is voted on live by another member and ends when its author says so', async ({ browser }) => {
  const ownerCtx = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  const memberCtx = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  const owner = await ownerCtx.newPage()
  const member = await memberCtx.newPage()
  try {
    await signIn(owner, ADMIN_USER, ADMIN_PASSWORD)
    const channel = await apiFetch(owner, 'POST', '/api/admin/channels', { name: CHANNEL, type: 'text' })
    expect(channel.status).toBe(201)
    const channelId = responseString(channel, 'id')

    await member.goto('/')
    if (suiteMember.registered) {
      expect((await apiFetch(member, 'POST', '/api/auth/login', { username: suiteMember.username, password: suiteMember.password })).status).toBe(200)
    } else {
      const invite = await apiFetch(owner, 'POST', '/api/admin/invites', { max_uses: 1 })
      expect((await apiFetch(member, 'POST', '/api/auth/register', { username: suiteMember.username, password: suiteMember.password, invite_code: responseString(invite, 'code') })).status).toBe(201)
      suiteMember.registered = true
    }
    await member.goto(`/c/${channelId}`)

    // /poll in the composer's command menu opens the poll form.
    await owner.goto(`/c/${channelId}`)
    const composer = owner.getByRole('textbox', { name: `Nachricht an #${CHANNEL}` })
    await composer.fill('/po')
    await expect(owner.getByTestId('slash-suggestions')).toContainText('/poll')
    await composer.press('Enter')
    const dialog = owner.getByTestId('poll-dialog')
    await dialog.getByLabel('Frage').fill('Pizza oder Pasta?')
    await dialog.getByTestId('poll-answer').nth(0).fill('Pizza')
    await dialog.getByTestId('poll-answer').nth(1).fill('Pasta')
    await dialog.getByTestId('poll-post').click()
    await expect(dialog).toBeHidden()

    const mine = owner.getByTestId('poll')
    await expect(mine).toContainText('Pizza oder Pasta?')
    await expect(mine.getByTestId('poll-total')).toHaveText('0 Stimmen')

    // The other member sees the poll arrive, votes, and the author sees it without reloading.
    const theirs = member.getByTestId('poll')
    await expect(theirs).toContainText('Pizza oder Pasta?')
    await theirs.locator('button', { hasText: 'Pasta' }).click()
    await expect(theirs.locator('button', { hasText: 'Pasta' })).toHaveAttribute('aria-pressed', 'true')
    await expect(mine.getByTestId('poll-total')).toHaveText('1 Stimme')
    await expect(mine.locator('button', { hasText: 'Pasta' })).toContainText('100%')
    await expect(mine.getByTestId('poll-voters')).toContainText(suiteMember.username)

    // Ending it early stops voting for everyone.
    await mine.getByTestId('poll-end').click()
    await owner.getByRole('alertdialog').getByRole('button', { name: 'Umfrage beenden' }).click()
    await expect(mine.getByTestId('poll-ended')).toBeVisible()
    await expect(theirs.getByTestId('poll-ended')).toBeVisible()
    await expect(theirs.locator('button', { hasText: 'Pizza' })).toBeDisabled()
  } finally {
    await ownerCtx.close()
    await memberCtx.close()
  }
})
