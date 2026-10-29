import { createHmac } from 'node:crypto'
import { test, expect } from './fixtures'
import type { Page } from '@playwright/test'
import { apiFetch, responseString, responseUserId } from './helpers'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'Admin'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)
const MEMBER = `second_${suffix}`
const PASSWORD = `pw-${suffix}-two-factor-test`

// RFC 6238, independent of the app: base32 secret in, six digits out.
function base32(text: string): Buffer {
  const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567'
  let bits = ''
  for (const ch of text.replace(/=+$/, '')) bits += alphabet.indexOf(ch).toString(2).padStart(5, '0')
  const bytes: number[] = []
  for (let i = 0; i + 8 <= bits.length; i += 8) bytes.push(parseInt(bits.slice(i, i + 8), 2))
  return Buffer.from(bytes)
}

function totp(secret: string, stepOffset = 0): string {
  const counter = Buffer.alloc(8)
  counter.writeBigUInt64BE(BigInt(Math.floor(Date.now() / 30_000) + stepOffset))
  const sum = createHmac('sha1', base32(secret)).update(counter).digest()
  const off = (sum[sum.length - 1] ?? 0) & 0x0f
  const n = sum.readUInt32BE(off) & 0x7fffffff
  return String(n % 1_000_000).padStart(6, '0')
}

async function signIn(page: Page, username: string, password: string) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByRole('heading', { name: 'Mnema Talk' })).toBeVisible()
  await dialog.getByLabel('Benutzername').fill(username)
  await dialog.getByLabel('Passwort').fill(password)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
}

test('a member turns on two-factor sign-in and then needs the code to sign in', async ({ browser }) => {
  const aCtx = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  const mCtx = await browser.newContext({ viewport: { width: 1400, height: 900 } })
  const a = await aCtx.newPage()
  const m = await mCtx.newPage()
  let memberId = ''
  try {
    await signIn(a, ADMIN_USER, ADMIN_PASSWORD)
    await expect(a.getByTestId('account-menu-button')).toBeVisible()
    const invite = await apiFetch(a, 'POST', '/api/admin/invites', { max_uses: 1 })
    await m.goto('/')
    const registered = await apiFetch(m, 'POST', '/api/auth/register', { username: MEMBER, password: PASSWORD, invite_code: responseString(invite, 'code') })
    expect(registered.status).toBe(201)
    memberId = responseUserId(await apiFetch(m, 'GET', '/api/auth/me'))
    await m.reload()

    // Turn it on: password, QR code and key, first code, recovery codes.
    await m.getByTestId('account-menu-button').click()
    await m.getByTestId('menu-settings').click()
    await m.getByTestId('two-factor-start').click()
    await m.getByTestId('two-factor-password').fill(PASSWORD)
    await m.getByTestId('two-factor-continue').click()
    await expect(m.getByTestId('qr-code')).toBeVisible()
    const secret = (await m.getByTestId('two-factor-secret').innerText()).trim()
    await m.getByTestId('two-factor-code').fill(totp(secret))
    await m.getByTestId('two-factor-enable').click()
    await expect(m.getByTestId('recovery-code-list').locator('li')).toHaveCount(10)
    await m.getByTestId('recovery-saved').check()
    await m.getByTestId('recovery-done').click()
    await expect(m.getByTestId('two-factor-state')).toContainText('An')

    // A fresh browser: the password alone no longer signs in.
    expect((await apiFetch(m, 'POST', '/api/auth/logout')).status).toBe(204)
    await signIn(m, MEMBER, PASSWORD)
    await expect(m.getByTestId('second-factor')).toBeVisible()
    await expect(m.getByTestId('account-menu-button')).toHaveCount(0)

    await m.getByTestId('second-factor-code').fill('000000')
    await m.getByTestId('second-factor-code').press('Enter')
    await expect(m.getByRole('alert')).toBeVisible()
    await expect(m.getByTestId('account-menu-button')).toHaveCount(0)

    // The code of the setup step is spent; the next step's code works.
    await m.getByTestId('second-factor-code').fill(totp(secret, 1))
    await m.getByTestId('second-factor-code').press('Enter')
    await expect(m.getByTestId('account-menu-button')).toBeVisible()
  } finally {
    // Leave no second factor behind, whatever happened above.
    if (memberId) await apiFetch(a, 'POST', `/api/admin/users/${memberId}/two-factor/reset`, { current_password: ADMIN_PASSWORD })
    await aCtx.close()
    await mCtx.close()
  }
})
