import { test, expect } from './fixtures'
import { apiFetch, responseString } from './helpers'

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'Herzog'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

const suffix = Date.now().toString(36)

test('a bot is a member: BOT tag and Bot role in the member list, locked role, online while connected', async ({ browser }) => {
  const page = await (await browser.newContext({ viewport: { width: 1400, height: 900 } })).newPage()
  await page.goto('/')
  expect((await apiFetch(page, 'POST', '/api/auth/login', { username: ADMIN_USER, password: ADMIN_PASSWORD })).status).toBe(200)

  const NAME = `Wachbot ${suffix}`
  const created = await apiFetch(page, 'POST', '/api/admin/bots', { name: NAME, allowed_intents: 0 })
  expect(created.status).toBe(201)
  const botId = responseString(created, 'id')
  const token = responseString(await apiFetch(page, 'POST', `/api/admin/bots/${botId}/tokens`, { name: 'e2e' }), 'secret')

  try {
    await page.goto('/')
    // Offline: listed with the BOT tag among the offline members.
    const row = page.locator('[data-member]').filter({ hasText: NAME })
    await expect(row).toBeVisible()
    await expect(row.getByTestId('bot-badge')).toHaveText(/bot/i)
    await expect(page.locator('[data-role-group="Bot"]')).toHaveCount(0)

    // Its profile: name, BOT tag, the Bot role (locked) and who added it.
    await row.click()
    await expect(page.getByTestId('profile-roles')).toContainText('Bot')
    await expect(page.getByTestId('profile-added-by')).toContainText(ADMIN_USER)
    await expect(page.getByTestId('profile-edit-roles')).toHaveCount(0)
    await page.keyboard.press('Escape')

    // Online while a gateway connection is open: grouped under the hoisted Bot role.
    await page.evaluate(({ tok }) => {
      const ws = new WebSocket(`${location.origin.replace(/^http/, 'ws')}/api/bot/v1/gateway`)
      ;(window as unknown as { botSocket: WebSocket }).botSocket = ws
      ws.onmessage = ev => {
        const frame = JSON.parse(String(ev.data)) as { op: number }
        if (frame.op === 10) ws.send(JSON.stringify({ op: 2, d: { token: tok, intents: 0, properties: {} } }))
      }
    }, { tok: token })
    const group = page.locator('[data-role-group="Bot"]')
    await expect(group).toBeVisible()
    await expect(group.getByTestId('bot-badge')).toBeVisible()
    await expect(group).toContainText(NAME)
    await page.evaluate(() => (window as unknown as { botSocket: WebSocket }).botSocket.close())
    await expect(group).toHaveCount(0)
    await expect(row).toBeVisible()

    // The role cannot be removed or deleted in the interface.
    await page.goto('/admin/roles')
    await page.locator('[data-role="Bot"] button[aria-pressed]').click()
    await expect(page.getByTestId('role-managed-note')).toBeVisible()
    await expect(page.getByTestId('role-managed-icon')).toBeVisible()
    await expect(page.locator('input[maxlength="32"]')).toBeDisabled()
    await expect(page.getByRole('button', { name: /Rolle löschen|Delete role/ })).toHaveCount(0)

    await page.goto('/admin/bots')
    await page.getByTestId('bot-row').filter({ hasText: NAME }).click()
    const checkbox = page.locator('[data-testid="bot-roles"] [data-role="Bot"] input[type="checkbox"]')
    await expect(checkbox).toBeChecked()
    await expect(checkbox).toBeDisabled()
  } finally {
    await apiFetch(page, 'DELETE', `/api/admin/bots/${botId}`)
  }
})
