import type { Page } from '@playwright/test'
import { apiFetch } from './helpers'
import { test, expect } from './fixtures'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'cool_guy'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
const CHANNEL = `invite-${suffix}`
const JOINER = `joiner${suffix}`
const JOINER_PASSWORD = `joiner-password-${suffix}`

async function signIn(page: Page, username: string, password: string) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('Benutzername').fill(username)
  await dialog.getByLabel('Passwort').fill(password)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
}

test('right-click a channel, invite people, and the joiner lands in that channel', async ({ browser }) => {
  const owner = await (await browser.newContext()).newPage()
  await signIn(owner, ADMIN_USER, ADMIN_PASSWORD)
  expect((await apiFetch(owner, 'POST', '/api/admin/channels', { name: CHANNEL, type: 'text' })).status).toBe(201)
  await owner.reload()

  await owner.locator('[data-channel-type="text"]', { hasText: CHANNEL }).click({ button: 'right' })
  await owner.getByRole('menuitem', { name: 'Leute einladen' }).click()
  const dialog = owner.getByRole('dialog', { name: `Leute in ${CHANNEL} einladen` })
  const link = dialog.getByTestId('channel-invite-link')
  await expect(link).toHaveValue(/\/invite\/[a-z0-9]{12}$/)
  const url = await link.inputValue()

  // A visitor opens the link, signs up with the code it carries and lands in the channel.
  const visitor = await (await browser.newContext()).newPage()
  await visitor.goto(new URL(new URL(url).pathname, owner.url()).toString())
  const signUp = visitor.getByRole('dialog')
  await signUp.getByLabel('Benutzername').fill(JOINER)
  await signUp.getByLabel('Passwort').fill(JOINER_PASSWORD)
  await signUp.getByRole('button', { name: 'Registrieren' }).click()
  await expect(visitor.getByTestId('chat-channel-name')).toHaveText(CHANNEL)
  await expect(visitor).toHaveURL(new RegExp(`/c/${CHANNEL}$`))
})
