import AxeBuilder from '@axe-core/playwright'
import type { Page } from '@playwright/test'
import { apiFetch, responseString } from './helpers'
import { test, expect } from './fixtures'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'cool_guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const CHANNEL = `a11y-${Date.now().toString(36)}`

// Every axe rule tagged WCAG 2.0/2.1/2.2 A or AA, with nothing excluded.
const TAGS = ['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa']

// within narrows the check to one dialog: what lies under two scrims (a dialog over the
// admin console over the chat) is not meant to be read, and its dimmed colours are not the app's.
async function expectNoViolations(page: Page, where: string, within?: string) {
  await page.waitForTimeout(400) // entry animations finished: axe reads the settled colours
  const builder = new AxeBuilder({ page }).withTags(TAGS)
  const result = await (within ? builder.include(within) : builder).analyze()
  // The failure summary says what axe measured (for contrast: both colours and the ratio).
  const found = result.violations.map(v => `${v.id}: ${v.nodes.slice(0, 3).map(n => `${n.target.join(' ')} ${n.html.slice(0, 140)} ${(n.failureSummary ?? '').replace(/\s+/g, ' ').slice(0, 220)}`).join(' | ')}`)
  expect(found, `accessibility violations on ${where}`).toEqual([])
}

async function signIn(page: Page) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByTestId('login-heading')).toBeVisible()
  await expectNoViolations(page, 'the sign-in dialog')
  await dialog.getByLabel('Benutzername').fill(ADMIN_USER)
  await dialog.getByLabel('Passwort').fill(ADMIN_PASSWORD)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

test('the main surfaces meet WCAG AA: chat, menus, settings, admin', async ({ page }) => {
  await signIn(page)
  const channel = await apiFetch(page, 'POST', '/api/admin/channels', { name: CHANNEL, type: 'text' })
  expect(channel.status).toBe(201)
  const id = responseString(channel, 'id')
  expect((await apiFetch(page, 'POST', '/api/admin/channels', { name: `${CHANNEL}-talk`, type: 'voice' })).status).toBe(201)
  expect((await apiFetch(page, 'POST', `/api/channels/${id}/messages`, { content: 'Hello **world** with `code`' })).status).toBe(201)
  // A message with a lifetime carries its remaining-time mark (doc/timed-messages.md).
  expect((await apiFetch(page, 'POST', `/api/channels/${id}/messages`, { content: 'Short-lived note', lifetime_seconds: 3600 })).status).toBe(201)
  await page.goto(`/c/${id}`)
  await expect(page.getByRole('article').filter({ hasText: 'Hello' })).toBeVisible()
  await expectNoViolations(page, 'the chat')
  await expect(page.getByTestId('lifetime-mark').first()).toBeVisible()
  await page.getByTestId('lifetime-button').click()
  await expect(page.getByRole('menu')).toBeVisible()
  await expectNoViolations(page, 'the composer timer')
  await page.keyboard.press('Escape')
  // The thread composer has the same timer, remembered for the thread on its own.
  const hello = page.locator('[data-msg-id]', { hasText: 'Hello' }).first()
  await hello.hover()
  await hello.getByTestId('msg-open-thread').click()
  const threadTimer = page.locator('aside').filter({ has: page.getByTestId('thread-close') }).getByTestId('lifetime-button')
  await threadTimer.click()
  await page.getByRole('menuitem', { name: '1 Std', exact: true }).click()
  // The narrow thread panel keeps the timer an icon: the choice is in its name and the help line.
  await expect(threadTimer).toHaveAttribute('aria-label', /1 Std/)
  await expect(page.locator('aside').filter({ has: page.getByTestId('thread-close') }).getByTestId('lifetime-help')).toBeVisible()
  await expectNoViolations(page, 'the thread composer timer')
  await page.getByTestId('thread-close').click()

  await page.getByRole('article').filter({ hasText: 'Hello' }).click({ button: 'right' })
  await expect(page.getByRole('menu')).toBeVisible()
  await expectNoViolations(page, 'the message menu')
  await page.keyboard.press('Escape')

  await page.getByRole('button', { name: /Konto-Menü|Konto/ }).last().click()
  await page.getByRole('menuitem', { name: 'Einstellungen' }).click()
  await expect(page.getByRole('dialog', { name: 'Einstellungen' })).toBeVisible()
  await expectNoViolations(page, 'the settings (sign-in)')
  for (const tab of ['sessions', 'privacy', 'data', 'audio']) {
    await page.getByTestId(`settings-tab-${tab}`).click()
    await expect(page.getByTestId(`settings-tab-${tab}`)).toHaveAttribute('aria-selected', 'true')
    await expectNoViolations(page, `the settings (${tab})`)
  }
  await page.keyboard.press('Escape')

  for (const tab of ['users', 'roles', 'channels', 'invites', 'messages']) {
    await page.goto(`/admin/${tab}`)
    await expect(page.getByTestId('admin-tabs')).toBeVisible()
    await expectNoViolations(page, `admin ${tab}`)
  }
})

test('adding a bot from its manifest meets WCAG AA: the confirm screen and the token', async ({ page }) => {
  await signIn(page)
  const channel = await apiFetch(page, 'POST', '/api/admin/channels', { name: `${CHANNEL}-bots`, type: 'text' })
  expect(channel.status).toBe(201)
  const name = `A11y bot ${Date.now().toString(36)}`.slice(0, 24)
  const manifest = {
    manifest_version: 1, name, description: 'Answers questions.', homepage: 'https://example.org/bot',
    intents: ['GUILDS'], permissions: ['view_channel', 'send_messages', 'mention_everyone'],
    commands: [{ name: 'poll', description: 'A poll of its own', options: [] }]
  }
  await page.goto(`/admin/bots/add#manifest=${Buffer.from(JSON.stringify(manifest), 'utf8').toString('base64url')}`)
  const confirm = page.getByTestId('manifest-confirm')
  await expect(confirm).toBeVisible()
  await expectNoViolations(page, 'the manifest confirm screen')
  await confirm.locator(`[data-channel="${responseString(channel, 'id')}"]`).check()
  await confirm.getByTestId('manifest-install').click()
  await expect(page.getByTestId('bot-secret')).toBeVisible()
  await expectNoViolations(page, 'the token dialog', '[role="dialog"]:has([data-testid="bot-secret"])')
  await page.getByTestId('bot-secret-done').click()
  // The new bot in the member list (its BOT tag), with nothing in front of it.
  await page.goto(`/c/${responseString(channel, 'id')}`)
  await expect(page.getByTestId('bot-badge').first()).toBeVisible()
  await expectNoViolations(page, 'the chat with a bot in the member list')
  const bots = await apiFetch(page, 'GET', '/api/admin/bots')
  const botId = (bots.json as { id: string; name: string }[]).find(b => b.name === name)?.id
  if (botId) expect((await apiFetch(page, 'DELETE', `/api/admin/bots/${botId}`)).status).toBe(204)
})
