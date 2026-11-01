import type { Page } from '@playwright/test'
import { awaitApiBudget } from './helpers'
import { test, expect } from './fixtures'

// "Test my connection" (Admin > Status) against the e2e stack: it reaches the
// voice port directly, and through the built-in TURN relay over UDP and TCP;
// with a browser limited to the relay the verdict is "only the relay works".
// It uses data channels only, so no microphone is involved. Runs in CI.

const ADMIN_USER = process.env.E2E_ADMIN_USER || 'Herzog'
const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD ?? ''
if (!ADMIN_PASSWORD) throw new Error('E2E_ADMIN_PASSWORD is required (run via e2e/run.sh)')

// Every peer connection of the page is created with iceTransportPolicy 'relay'
// (a browser behind a firewall that drops direct UDP).
const forceRelay = () => {
  const Native = window.RTCPeerConnection
  window.RTCPeerConnection = class extends Native {
    constructor(...args: ConstructorParameters<typeof RTCPeerConnection>) {
      super({ ...(args[0] ?? {}), iceTransportPolicy: 'relay' })
    }
  }
}

async function openStatus(page: Page) {
  await page.goto('/')
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('Benutzername').fill(ADMIN_USER)
  await dialog.getByLabel('Passwort').fill(ADMIN_PASSWORD)
  await dialog.getByRole('button', { name: 'Anmelden' }).click()
  await expect(dialog).toBeHidden()
  if (await awaitApiBudget(page)) await page.reload()
  await page.goto('/admin/status')
  await expect(page.getByTestId('status-connection-test')).toBeVisible()
}

test('Test my connection reaches the voice port directly and through the relay over UDP and TCP', async ({ page, browserName }) => {
  test.skip(browserName === 'webkit', 'Playwright WebKit does not gather the loopback candidates the harness announces')
  test.setTimeout(150_000)
  await openStatus(page)

  // The WebSocket report of the opened tab turned the item green.
  await expect(page.getByTestId('setup-check-websocket')).toHaveAttribute('data-level', 'ok', { timeout: 30_000 })

  await page.getByTestId('connection-test-run').click()
  await expect(page.getByTestId('connection-test-verdict')).toBeVisible({ timeout: 120_000 })
  await expect(page.getByTestId('connection-test-verdict')).toHaveAttribute('data-kind', 'direct')
  for (const path of ['direct', 'relayUdp', 'relayTcp']) {
    await expect(page.getByTestId(`connection-test-path-${path}`), path).toHaveAttribute('data-outcome', 'ok')
  }

  // The harness is the server's own network, so the upload cannot be measured and the test says so.
  await expect(page.getByTestId('connection-test-speed')).toHaveAttribute('data-outcome', 'lan')

  // The test made no presence and no Talk, and a peer on the harness's own
  // loopback address is not "from outside": the voice port stays neutral.
  await page.getByTestId('admin-status').getByRole('button', { name: 'Aktualisieren' }).click()
  await expect(page.getByTestId('setup-check-voice_udp')).toHaveAttribute('data-level', 'info')
})

test('with a browser limited to the relay, the verdict is that only the relay works', async ({ browser, browserName }) => {
  test.skip(browserName === 'webkit', 'Playwright WebKit does not gather the loopback candidates the harness announces')
  test.setTimeout(180_000)
  const context = await browser.newContext({ locale: 'de-DE' })
  await context.addInitScript(forceRelay)
  const page = await context.newPage()
  await openStatus(page)

  await page.getByTestId('connection-test-run').click()
  await expect(page.getByTestId('connection-test-verdict')).toBeVisible({ timeout: 150_000 })
  await expect(page.getByTestId('connection-test-verdict')).toHaveAttribute('data-kind', 'relay_only')
  await expect(page.getByTestId('connection-test-path-direct')).toHaveAttribute('data-outcome', 'fail')
  await expect(page.getByTestId('connection-test-path-relayUdp')).toHaveAttribute('data-outcome', 'ok')
  await expect(page.getByTestId('connection-test-cause')).toHaveAttribute('data-cause', 'udp_not_forwarded')
  await context.close()
})
