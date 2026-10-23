import { test as base, expect } from '@playwright/test'
import type { Browser, BrowserContext } from '@playwright/test'
import { explainNavigationFailures } from './navigation-diagnostics'

// Every spec signs in as the Owner from one address, and the API allows 600
// requests a minute per account. Instead of failing (or sleeping) after the
// server answered 429, each test first waits until the browsers of this run
// have sent few enough API requests in the last minute to leave it room.
// Production limits are untouched: this only paces the tests.

const WINDOW_MS = 60_000
// Requests one account may have sent in the previous minute before a test is
// allowed to start; the test itself then still has the rest of the 600.
const HEADROOM = 400
// A fixed window always ends within a minute, so waiting longer is pointless.
const MAX_WAIT_MS = WINDOW_MS + 5_000

// API request times per account (the id inside the session cookie), because
// the limit is per account. Requests without a session count as "anonymous".
const sent = new Map<string, number[]>()

function accountOf(cookie: string | undefined): string {
  const token = /(?:^|;\s*)mnema_session=([^;]+)/.exec(cookie ?? '')?.[1]
  const payload = token?.split('.')[1]
  if (!payload) return 'anonymous'
  try {
    const claims: unknown = JSON.parse(Buffer.from(payload, 'base64url').toString('utf8'))
    const uid = typeof claims === 'object' && claims !== null ? Object.getOwnPropertyDescriptor(claims, 'uid')?.value : undefined
    return typeof uid === 'string' ? uid : 'anonymous'
  } catch {
    return 'anonymous'
  }
}

/** The most API requests any one account sent in the last minute. */
function busiestAccount(now = Date.now()): number {
  let busiest = 0
  for (const times of sent.values()) {
    while (times.length > 0 && now - (times[0] ?? now) > WINDOW_MS) times.shift()
    busiest = Math.max(busiest, times.length)
  }
  return busiest
}

/** Waits until no account used more than the headroom of its budget in the last minute. */
export async function awaitApiHeadroom(headroom = HEADROOM): Promise<void> {
  const started = Date.now()
  while (busiestAccount() > headroom && Date.now() - started < MAX_WAIT_MS) {
    await new Promise((resolve) => setTimeout(resolve, 500))
  }
}

function countApiRequests(context: BrowserContext): void {
  context.on('request', (request) => {
    if (!new URL(request.url()).pathname.startsWith('/api/')) return
    const at = Date.now()
    void request.allHeaders().then((headers) => {
      const account = accountOf(headers['cookie'])
      const times = sent.get(account) ?? []
      times.push(at)
      sent.set(account, times)
    }, () => undefined)
  })
}

// The page's Content-Security-Policy (style-src 'self', style-src-attr 'none')
// is part of what ships, so no spec may trigger a violation. Every context
// reports securitypolicyviolation events from all of its frames (and shadow
// trees, whose events reach the document) through a binding, and the browser's
// own "Refused to ..." console messages are collected too. An auto fixture
// fails the test that caused one.
const CSP_BINDING = '__mnemaCspViolation'
const CSP_LISTENER = `(() => {
  const report = (e) => {
    let text = [e.violatedDirective, e.blockedURI || 'inline', e.sourceFile ? e.sourceFile + ':' + e.lineNumber : '', (e.sample || '').slice(0, 80)].join(' ')
    // The page has a select, whose shadow tree WebKit 27 reports (see WEBKIT_SELECT_EVENT).
    if (e.violatedDirective === 'style-src-attr' && document.querySelector('select')) text += ' [select]'
    try { window.${CSP_BINDING}(text) } catch { /* page is going away */ }
  }
  document.addEventListener('securitypolicyviolation', report, true)
  window.addEventListener('securitypolicyviolation', report, true)
})()`
const CSP_CONSOLE = /Content[- ]Security[- ]Policy|Refused to (apply|load|execute|create|connect|frame|evaluate)|violates the following/i

let cspViolations: string[] = []
// Playwright's WebKit injects an inline stylesheet of its own when it takes
// the failure screenshot, which the policy rightly refuses and reports with no
// source file. That one signature is ignored in WebKit; a <style> element of
// the app is still caught by the Chromium and Firefox runs, and every other
// violation (style attributes, scripts, connections) counts in all engines.
const WEBKIT_SCREENSHOT_EVENT = /^style-src-elem inline\s*$/
// WebKit 27.2 (Playwright 1.64) reports one style-src-attr violation for every
// <select> it builds: the control's own shadow tree sets an inline style (empty
// sample; the script that created the select is named as the source). Nothing
// is blocked: a page with selects renders pixel-identically under
// 'unsafe-inline' and under style-src-attr 'none', and WebKit 26.6 does not
// report it. The policy therefore stays as it is, and in WebKit only this
// signature (a style-src-attr event on a page that has a select) is ignored.
// The same code runs in the Chromium and Firefox jobs, which still fail on any
// style-src-attr event, so an attribute of the app cannot slip through. See
// doc/product-status.md (Browsers).
const WEBKIT_SELECT_EVENT = /^style-src-attr inline .*\[select\]$/
const WEBKIT_SCREENSHOT_CONSOLE = /^Refused to apply a stylesheet because its hash, its nonce, or 'unsafe-inline' does not appear/

async function watchCsp(context: BrowserContext): Promise<void> {
  const webkit = context.browser()?.browserType().name() === 'webkit'
  await context.exposeBinding(CSP_BINDING, (_source, text: string) => {
    if (!(webkit && (WEBKIT_SCREENSHOT_EVENT.test(text) || WEBKIT_SELECT_EVENT.test(text)))) cspViolations.push(`event: ${text}`)
  })
  await context.addInitScript(CSP_LISTENER)
  context.on('console', (message) => {
    const text = message.text()
    if (CSP_CONSOLE.test(text) && !(webkit && WEBKIT_SCREENSHOT_CONSOLE.test(text))) cspViolations.push(`console: ${text}`)
  })
}

// Every test registers or signs in accounts that are new to the welcome flow,
// whose dialog would cover the page under test. Contexts therefore see it as
// already finished; the onboarding spec opts back in with showWelcomeFlow.
const ONBOARDING_STATE = '**/api/onboarding'

async function hideWelcomeFlow(context: BrowserContext): Promise<void> {
  await context.route(ONBOARDING_STATE, async (route) => {
    if (route.request().method() !== 'GET') return route.fallback()
    const response = await route.fetch()
    const state: unknown = await response.json().catch(() => null)
    if (typeof state !== 'object' || state === null) return route.fulfill({ response })
    return route.fulfill({ response, json: { ...state, welcome_done: true } })
  })
}

/** Lets the welcome flow appear in this context, as it does for a real new member. */
export async function showWelcomeFlow(context: BrowserContext): Promise<void> {
  await context.unroute(ONBOARDING_STATE)
}

// Firefox has no context-level media grants (its fake devices need none, see
// firefoxUserPrefs in playwright.config.ts) and rejects the names outright.
function grantable(browser: Browser, options: Parameters<Browser['newContext']>[0]): Parameters<Browser['newContext']>[0] {
  if (browser.browserType().name() !== 'firefox' || !options?.permissions) return options
  const permissions = options.permissions.filter(name => name !== 'microphone' && name !== 'camera')
  return { ...options, permissions }
}

// WebKit lets one page of a browser process capture the microphone and mutes
// the others, as Safari does with its tabs. Two members of a call in one test
// therefore each get their own WebKit process, which is also how two people
// would join: from two devices.
async function contextOwnProcess(browser: Browser, options: Parameters<Browser['newContext']>[0]): Promise<BrowserContext> {
  const own = await browser.browserType().launch()
  const context = await own.newContext(options)
  const close = context.close.bind(context)
  context.close = async (...args: Parameters<BrowserContext['close']>) => {
    await close(...args)
    await own.close()
  }
  return context
}

// The server tells clients apart by address (and trusts the loopback proxy's
// X-Forwarded-For), and its sign-in budget of 60 per 15 minutes is per address.
// The whole suite runs from one address, so it sits right at that budget and
// every new sign-in pushes later specs over it. Each browser context therefore
// presents its own (from the benchmarking range, never routed) client address, like
// the separate people it stands for. Production limits are untouched.
// A service worker answers the page's requests itself, which context.route
// (the welcome-flow stand-in below) does not see in WebKit and Firefox. Contexts
// therefore block it, except in the specs about the worker itself. (Playwright
// drops a serviceWorkers option that equals its own default, so the specs are
// named here instead of opting in through test.use.)
const SERVICE_WORKER_SPECS = /[\\/](pwa|push)\.spec\.ts$/

function serviceWorkersFor(): 'allow' | 'block' {
  try {
    return SERVICE_WORKER_SPECS.test(base.info().file) ? 'allow' : 'block'
  } catch {
    return 'block'
  }
}

let nextClient = 1

function clientAddress(): string {
  const n = nextClient++
  return `198.18.${Math.floor(n / 250) % 250}.${(n % 250) + 1}`
}

function counted(browser: Browser): Browser {
  const newContext = browser.newContext.bind(browser)
  const separate = browser.browserType().name() === 'webkit'
  browser.newContext = async (...args: Parameters<Browser['newContext']>) => {
    const options = args[0] ?? {}
    const addressed = grantable(browser, { serviceWorkers: serviceWorkersFor(), ...options, extraHTTPHeaders: { 'X-Forwarded-For': clientAddress(), ...(options.extraHTTPHeaders ?? {}) } })
    const context = await (separate ? contextOwnProcess(browser, addressed) : newContext(addressed))
    countApiRequests(context)
    await watchCsp(context)
    await hideWelcomeFlow(context)
    explainNavigationFailures(context)
    return context
  }
  return browser
}

export const test = base.extend<{ apiHeadroom: void; cspViolations: void }, { browser: Browser }>({
  browser: [async ({ browser }, use) => { await use(counted(browser)) }, { scope: 'worker' }],
  // Playwright insists on a destructuring pattern, even an empty one.
  // eslint-disable-next-line no-empty-pattern
  apiHeadroom: [async ({}, use) => { await awaitApiHeadroom(); await use() }, { auto: true }],
  // eslint-disable-next-line no-empty-pattern
  cspViolations: [async ({}, use) => {
    cspViolations = []
    await use()
    const seen = [...new Set(cspViolations)]
    cspViolations = []
    expect(seen, 'Content-Security-Policy violations').toEqual([])
  }, { auto: true }],
})

export { expect }
