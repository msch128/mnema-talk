import { test, expect, isOpen } from './fixtures'
import type { BrowserContext } from '@playwright/test'

// A context a test opens and never closes must not outlive the test: left open,
// it stays signed in, keeps answering broadcasts with API requests and, in
// WebKit, keeps a browser process of its own busy for the rest of the worker
// (see testContexts in fixtures.ts). A suite's beforeAll context is shared by
// its tests and stays until afterAll.
test.describe.configure({ mode: 'serial' })

let shared: BrowserContext | undefined
let leaked: BrowserContext | undefined

test.beforeAll(async ({ browser }) => {
  shared = await browser.newContext()
})

test.afterAll(async () => {
  await shared?.close()
})

test('a test leaves a context open', async ({ browser }) => {
  leaked = await browser.newContext()
  const page = await leaked.newPage()
  await page.goto('/')
  expect(isOpen(leaked)).toBe(true)
})

test('the context the test before left open is closed, the suite’s context is not', async ({ browserName }) => {
  if (!leaked || !shared) throw new Error('the test before did not run')
  expect(isOpen(leaked)).toBe(false)
  // In WebKit every context has a browser process of its own; it ends with the context.
  if (browserName === 'webkit') expect(leaked.browser()?.isConnected() ?? false).toBe(false)
  expect(isOpen(shared)).toBe(true)
  const page = await shared.newPage()
  await page.goto('/')
  await expect(page.getByRole('dialog').getByTestId('login-heading')).toBeVisible()
})
